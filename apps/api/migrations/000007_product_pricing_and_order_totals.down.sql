ALTER TABLE order_items
    DROP CONSTRAINT IF EXISTS order_items_pricing_snapshot_group_check,
    DROP CONSTRAINT IF EXISTS order_items_line_total_minor_check,
    DROP CONSTRAINT IF EXISTS order_items_currency_code_snapshot_check,
    DROP CONSTRAINT IF EXISTS order_items_unit_price_minor_snapshot_check,
    DROP COLUMN IF EXISTS unit_price_minor_snapshot,
    DROP COLUMN IF EXISTS currency_code_snapshot,
    DROP COLUMN IF EXISTS line_total_minor;

ALTER TABLE orders
    DROP CONSTRAINT IF EXISTS orders_totals_pair_check,
    DROP CONSTRAINT IF EXISTS orders_subtotal_minor_check,
    DROP CONSTRAINT IF EXISTS orders_currency_code_check,
    DROP COLUMN IF EXISTS currency_code,
    DROP COLUMN IF EXISTS subtotal_minor;

ALTER TABLE products
    DROP CONSTRAINT IF EXISTS products_pricing_pair_check,
    DROP CONSTRAINT IF EXISTS products_currency_code_check,
    DROP CONSTRAINT IF EXISTS products_unit_price_minor_check,
    DROP COLUMN IF EXISTS unit_price_minor,
    DROP COLUMN IF EXISTS currency_code;
