DROP INDEX IF EXISTS inventory_reservations_order_item_idx;
ALTER TABLE inventory_reservations
    DROP CONSTRAINT IF EXISTS inventory_reservations_order_item_fk,
    DROP COLUMN IF EXISTS order_item_id;
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
ALTER TABLE products
    DROP CONSTRAINT IF EXISTS products_organization_id_id_key;