BEGIN;

ALTER TABLE orders DROP CONSTRAINT orders_status_check;
ALTER TABLE orders
    ADD CONSTRAINT orders_status_check CHECK (status IN ('pending','cancelled','paid','partially_fulfilled','fulfilled')),
    ADD COLUMN fulfilled_at TIMESTAMPTZ;

ALTER TABLE inventory_reservations DROP CONSTRAINT inventory_reservations_status_check;
ALTER TABLE inventory_reservations
    ADD CONSTRAINT inventory_reservations_status_check
    CHECK (status IN ('active','released','expired','payment_held','committed','consumed'));

ALTER TABLE order_items
    ADD CONSTRAINT order_items_organization_id_id_order_id_key UNIQUE (organization_id, id, order_id);

ALTER TABLE inventory_levels
    ADD CONSTRAINT inventory_levels_organization_id_id_warehouse_id_key UNIQUE (organization_id, id, warehouse_id);

ALTER TABLE warehouses
    ADD CONSTRAINT warehouses_organization_id_id_key UNIQUE (organization_id, id);

ALTER TABLE inventory_reservations
    ADD CONSTRAINT inventory_reservations_consumption_identity_key
    UNIQUE (organization_id, id, order_item_id, inventory_level_id);

ALTER TABLE inventory_reservations
    ADD CONSTRAINT inventory_reservations_organization_id_id_key UNIQUE (organization_id, id);

CREATE TABLE fulfillments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    order_id UUID NOT NULL,
    warehouse_id UUID NOT NULL,
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'completed' CHECK (status = 'completed'),
    idempotency_key VARCHAR(200) NOT NULL,
    request_hash VARCHAR(64) NOT NULL CHECK (char_length(request_hash) = 64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, id, order_id),
    UNIQUE (organization_id, id, warehouse_id),
    UNIQUE (organization_id, idempotency_key),
    CONSTRAINT fulfillments_order_fk FOREIGN KEY (organization_id, order_id)
        REFERENCES orders (organization_id, id) ON DELETE CASCADE,
    CONSTRAINT fulfillments_warehouse_fk FOREIGN KEY (organization_id, warehouse_id)
        REFERENCES warehouses (organization_id, id) ON DELETE RESTRICT
);

CREATE INDEX fulfillments_organization_order_idx
    ON fulfillments (organization_id, order_id, created_at DESC);
CREATE INDEX fulfillments_organization_warehouse_idx
    ON fulfillments (organization_id, warehouse_id, created_at DESC);

CREATE TABLE fulfillment_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    fulfillment_id UUID NOT NULL,
    order_id UUID NOT NULL,
    order_item_id UUID NOT NULL,
    reservation_id UUID NOT NULL,
    inventory_level_id UUID NOT NULL,
    warehouse_id UUID NOT NULL,
    quantity BIGINT NOT NULL CHECK (quantity > 0),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, reservation_id),
    CONSTRAINT fulfillment_items_fulfillment_fk FOREIGN KEY (organization_id, fulfillment_id, order_id)
        REFERENCES fulfillments (organization_id, id, order_id) ON DELETE CASCADE,
    CONSTRAINT fulfillment_items_fulfillment_warehouse_fk FOREIGN KEY (organization_id, fulfillment_id, warehouse_id)
        REFERENCES fulfillments (organization_id, id, warehouse_id) ON DELETE CASCADE,
    CONSTRAINT fulfillment_items_order_item_fk FOREIGN KEY (organization_id, order_item_id, order_id)
        REFERENCES order_items (organization_id, id, order_id) ON DELETE RESTRICT,
    CONSTRAINT fulfillment_items_reservation_fk FOREIGN KEY (organization_id, reservation_id)
        REFERENCES inventory_reservations (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT fulfillment_items_inventory_fk FOREIGN KEY (organization_id, inventory_level_id, warehouse_id)
        REFERENCES inventory_levels (organization_id, id, warehouse_id)
);

CREATE INDEX fulfillment_items_organization_fulfillment_idx
    ON fulfillment_items (organization_id, fulfillment_id);
CREATE INDEX fulfillment_items_organization_order_idx
    ON fulfillment_items (organization_id, order_id, order_item_id);
CREATE INDEX fulfillment_items_organization_reservation_idx
    ON fulfillment_items (organization_id, reservation_id);

CREATE TABLE inventory_movements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    inventory_level_id UUID NOT NULL,
    product_id UUID NOT NULL,
    warehouse_id UUID NOT NULL,
    movement_type TEXT NOT NULL CHECK (movement_type IN ('fulfillment','adjustment')),
    quantity_delta BIGINT NOT NULL CHECK (quantity_delta <> 0),
    fulfillment_id UUID,
    reservation_id UUID,
    created_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT inventory_movements_inventory_fk FOREIGN KEY (organization_id, inventory_level_id, warehouse_id)
        REFERENCES inventory_levels (organization_id, id, warehouse_id) ON DELETE RESTRICT,
    CONSTRAINT inventory_movements_product_fk FOREIGN KEY (organization_id, product_id)
        REFERENCES products (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT inventory_movements_warehouse_fk FOREIGN KEY (organization_id, warehouse_id)
        REFERENCES warehouses (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT inventory_movements_fulfillment_fk FOREIGN KEY (organization_id, fulfillment_id)
        REFERENCES fulfillments (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT inventory_movements_reservation_fk FOREIGN KEY (organization_id, reservation_id)
        REFERENCES inventory_reservations (organization_id, id) ON DELETE RESTRICT,
    CONSTRAINT inventory_movements_shape_check CHECK (
        (movement_type = 'fulfillment' AND quantity_delta < 0 AND fulfillment_id IS NOT NULL AND reservation_id IS NOT NULL)
        OR
        (movement_type = 'adjustment' AND fulfillment_id IS NULL AND reservation_id IS NULL)
    )
);

CREATE INDEX inventory_movements_organization_inventory_idx
    ON inventory_movements (organization_id, inventory_level_id, created_at DESC);
CREATE INDEX inventory_movements_organization_fulfillment_idx
    ON inventory_movements (organization_id, fulfillment_id, created_at DESC);
CREATE UNIQUE INDEX inventory_movements_one_fulfillment_per_reservation_idx
    ON inventory_movements (organization_id, reservation_id)
    WHERE movement_type = 'fulfillment' AND reservation_id IS NOT NULL;

COMMIT;
