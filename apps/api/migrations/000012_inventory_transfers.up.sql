BEGIN;

CREATE TABLE inventory_transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    source_warehouse_id UUID NOT NULL,
    destination_warehouse_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','in_transit','completed','cancelled')),
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    dispatched_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    received_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    cancelled_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    dispatched_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    creation_idempotency_key VARCHAR(200) NOT NULL,
    creation_request_hash VARCHAR(64) NOT NULL CHECK (char_length(creation_request_hash) = 64),
    dispatch_idempotency_key VARCHAR(200),
    receive_idempotency_key VARCHAR(200),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, id, source_warehouse_id),
    UNIQUE (organization_id, id, destination_warehouse_id),
    UNIQUE (organization_id, creation_idempotency_key),
    CONSTRAINT inventory_transfers_source_warehouse_fk FOREIGN KEY (organization_id, source_warehouse_id)
        REFERENCES warehouses (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT inventory_transfers_destination_warehouse_fk FOREIGN KEY (organization_id, destination_warehouse_id)
        REFERENCES warehouses (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT inventory_transfers_different_warehouses_chk CHECK (source_warehouse_id <> destination_warehouse_id),
    CONSTRAINT inventory_transfers_timestamps_chk CHECK (
        (status = 'pending' AND dispatched_at IS NULL AND completed_at IS NULL AND cancelled_at IS NULL)
        OR (status = 'in_transit' AND dispatched_at IS NOT NULL AND completed_at IS NULL AND cancelled_at IS NULL)
        OR (status = 'completed' AND dispatched_at IS NOT NULL AND completed_at IS NOT NULL AND cancelled_at IS NULL)
        OR (status = 'cancelled' AND dispatched_at IS NULL AND completed_at IS NULL AND cancelled_at IS NOT NULL)
    )
);

CREATE TABLE inventory_transfer_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    transfer_id UUID NOT NULL,
    product_id UUID NOT NULL,
    source_inventory_level_id UUID NOT NULL,
    destination_inventory_level_id UUID NOT NULL,
    quantity BIGINT NOT NULL CHECK (quantity > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, transfer_id, product_id),
    UNIQUE (organization_id, transfer_id, id),
    CONSTRAINT inventory_transfer_items_transfer_fk FOREIGN KEY (organization_id, transfer_id)
        REFERENCES inventory_transfers (organization_id, id) ON DELETE CASCADE,
    CONSTRAINT inventory_transfer_items_product_fk FOREIGN KEY (organization_id, product_id)
        REFERENCES products (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT inventory_transfer_items_source_inventory_fk FOREIGN KEY (organization_id, source_inventory_level_id)
        REFERENCES inventory_levels (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT inventory_transfer_items_destination_inventory_fk FOREIGN KEY (organization_id, destination_inventory_level_id)
        REFERENCES inventory_levels (organization_id, id) ON DELETE RESTRICT
);

CREATE INDEX inventory_transfers_organization_status_idx ON inventory_transfers (organization_id, status, created_at DESC);
CREATE INDEX inventory_transfers_organization_source_idx ON inventory_transfers (organization_id, source_warehouse_id, created_at DESC);
CREATE INDEX inventory_transfers_organization_destination_idx ON inventory_transfers (organization_id, destination_warehouse_id, created_at DESC);
CREATE INDEX inventory_transfer_items_organization_transfer_idx ON inventory_transfer_items (organization_id, transfer_id, id);
CREATE INDEX inventory_transfer_items_organization_product_idx ON inventory_transfer_items (organization_id, product_id);

ALTER TABLE inventory_movements
    ADD COLUMN transfer_id UUID,
    ADD COLUMN transfer_item_id UUID;

ALTER TABLE inventory_movements DROP CONSTRAINT inventory_movements_shape_check;
ALTER TABLE inventory_movements DROP CONSTRAINT IF EXISTS inventory_movements_movement_type_check;
ALTER TABLE inventory_movements
    ADD CONSTRAINT inventory_movements_shape_check CHECK (
        (movement_type = 'fulfillment' AND quantity_delta < 0 AND fulfillment_id IS NOT NULL AND reservation_id IS NOT NULL AND transfer_id IS NULL AND transfer_item_id IS NULL)
        OR (movement_type = 'adjustment' AND fulfillment_id IS NULL AND reservation_id IS NULL AND transfer_id IS NULL AND transfer_item_id IS NULL)
        OR (movement_type = 'transfer_out' AND quantity_delta < 0 AND fulfillment_id IS NULL AND reservation_id IS NULL AND transfer_id IS NOT NULL AND transfer_item_id IS NOT NULL)
        OR (movement_type = 'transfer_in' AND quantity_delta > 0 AND fulfillment_id IS NULL AND reservation_id IS NULL AND transfer_id IS NOT NULL AND transfer_item_id IS NOT NULL)
    );
ALTER TABLE inventory_movements DROP CONSTRAINT inventory_movements_inventory_fk;
ALTER TABLE inventory_movements
    ADD CONSTRAINT inventory_movements_inventory_fk FOREIGN KEY (organization_id, inventory_level_id, warehouse_id)
        REFERENCES inventory_levels (organization_id, id, warehouse_id) ON DELETE RESTRICT,
    ADD CONSTRAINT inventory_movements_transfer_fk FOREIGN KEY (organization_id, transfer_id)
        REFERENCES inventory_transfers (organization_id, id) ON DELETE RESTRICT,
    ADD CONSTRAINT inventory_movements_transfer_item_fk FOREIGN KEY (organization_id, transfer_id, transfer_item_id)
        REFERENCES inventory_transfer_items (organization_id, transfer_id, id) ON DELETE RESTRICT;

CREATE INDEX inventory_movements_organization_transfer_idx ON inventory_movements (organization_id, transfer_id, created_at DESC);
CREATE UNIQUE INDEX inventory_movements_one_transfer_out_per_item_idx
    ON inventory_movements (organization_id, transfer_item_id)
    WHERE movement_type = 'transfer_out';
CREATE UNIQUE INDEX inventory_movements_one_transfer_in_per_item_idx
    ON inventory_movements (organization_id, transfer_item_id)
    WHERE movement_type = 'transfer_in';

COMMIT;
