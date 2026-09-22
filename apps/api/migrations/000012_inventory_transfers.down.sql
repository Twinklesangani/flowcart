BEGIN;

DROP INDEX IF EXISTS inventory_movements_one_transfer_in_per_item_idx;
DROP INDEX IF EXISTS inventory_movements_one_transfer_out_per_item_idx;
DROP INDEX IF EXISTS inventory_movements_organization_transfer_idx;
ALTER TABLE inventory_movements
    DROP CONSTRAINT IF EXISTS inventory_movements_transfer_item_fk,
    DROP CONSTRAINT IF EXISTS inventory_movements_transfer_fk,
    DROP CONSTRAINT IF EXISTS inventory_movements_inventory_fk;
ALTER TABLE inventory_movements
    ADD CONSTRAINT inventory_movements_inventory_fk FOREIGN KEY (organization_id, inventory_level_id, warehouse_id)
        REFERENCES inventory_levels (organization_id, id, warehouse_id) ON DELETE RESTRICT;
ALTER TABLE inventory_movements
    DROP CONSTRAINT inventory_movements_shape_check;
ALTER TABLE inventory_movements
    DROP CONSTRAINT IF EXISTS inventory_movements_movement_type_check;
ALTER TABLE inventory_movements
    ADD CONSTRAINT inventory_movements_movement_type_check CHECK (movement_type IN ('fulfillment','adjustment'));
ALTER TABLE inventory_movements
    ADD CONSTRAINT inventory_movements_shape_check CHECK (
        (movement_type = 'fulfillment' AND quantity_delta < 0 AND fulfillment_id IS NOT NULL AND reservation_id IS NOT NULL)
        OR (movement_type = 'adjustment' AND fulfillment_id IS NULL AND reservation_id IS NULL)
    );
ALTER TABLE inventory_movements
    DROP COLUMN transfer_item_id,
    DROP COLUMN transfer_id;

DROP TABLE IF EXISTS inventory_transfer_items;
DROP TABLE IF EXISTS inventory_transfers;

COMMIT;
