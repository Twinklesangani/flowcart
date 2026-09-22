BEGIN;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM fulfillments)
       OR EXISTS (SELECT 1 FROM inventory_movements)
       OR EXISTS (SELECT 1 FROM inventory_reservations WHERE status='consumed')
       OR EXISTS (SELECT 1 FROM orders WHERE status IN ('partially_fulfilled','fulfilled')) THEN
        RAISE EXCEPTION 'M13 downgrade refused: fulfillment history requires explicit archival';
    END IF;
END $$;

DROP TABLE inventory_movements;
DROP TABLE fulfillment_items;
DROP TABLE fulfillments;

ALTER TABLE inventory_reservations DROP CONSTRAINT inventory_reservations_consumption_identity_key;
ALTER TABLE inventory_reservations DROP CONSTRAINT inventory_reservations_organization_id_id_key;
ALTER TABLE order_items DROP CONSTRAINT order_items_organization_id_id_order_id_key;
ALTER TABLE inventory_levels DROP CONSTRAINT inventory_levels_organization_id_id_warehouse_id_key;
ALTER TABLE warehouses DROP CONSTRAINT warehouses_organization_id_id_key;

ALTER TABLE inventory_reservations DROP CONSTRAINT inventory_reservations_status_check;
ALTER TABLE inventory_reservations
    ADD CONSTRAINT inventory_reservations_status_check
    CHECK (status IN ('active','released','expired','payment_held','committed'));

ALTER TABLE orders DROP CONSTRAINT orders_status_check;
ALTER TABLE orders
    ADD CONSTRAINT orders_status_check CHECK (status IN ('pending','cancelled','paid'));
ALTER TABLE orders DROP COLUMN fulfilled_at;

COMMIT;
