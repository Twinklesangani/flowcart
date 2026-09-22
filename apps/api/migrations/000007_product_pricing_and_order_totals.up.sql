ALTER TABLE products
    ADD COLUMN unit_price_minor BIGINT,
    ADD COLUMN currency_code VARCHAR(3),
    ADD CONSTRAINT products_unit_price_minor_check CHECK (unit_price_minor IS NULL OR unit_price_minor >= 0),
    ADD CONSTRAINT products_currency_code_check CHECK (currency_code IS NULL OR currency_code ~ '^[A-Z]{3}$'),
    ADD CONSTRAINT products_pricing_pair_check CHECK ((unit_price_minor IS NULL AND currency_code IS NULL) OR (unit_price_minor IS NOT NULL AND currency_code IS NOT NULL));

ALTER TABLE orders
    ADD COLUMN currency_code VARCHAR(3),
    ADD COLUMN subtotal_minor BIGINT,
    ADD CONSTRAINT orders_currency_code_check CHECK (currency_code IS NULL OR currency_code ~ '^[A-Z]{3}$'),
    ADD CONSTRAINT orders_subtotal_minor_check CHECK (subtotal_minor IS NULL OR subtotal_minor >= 0),
    ADD CONSTRAINT orders_totals_pair_check CHECK ((currency_code IS NULL AND subtotal_minor IS NULL) OR (currency_code IS NOT NULL AND subtotal_minor IS NOT NULL));

ALTER TABLE order_items
    ADD COLUMN unit_price_minor_snapshot BIGINT,
    ADD COLUMN currency_code_snapshot VARCHAR(3),
    ADD COLUMN line_total_minor BIGINT,
    ADD CONSTRAINT order_items_unit_price_minor_snapshot_check CHECK (unit_price_minor_snapshot IS NULL OR unit_price_minor_snapshot >= 0),
    ADD CONSTRAINT order_items_currency_code_snapshot_check CHECK (currency_code_snapshot IS NULL OR currency_code_snapshot ~ '^[A-Z]{3}$'),
    ADD CONSTRAINT order_items_line_total_minor_check CHECK (line_total_minor IS NULL OR line_total_minor >= 0),
    ADD CONSTRAINT order_items_pricing_snapshot_group_check CHECK ((unit_price_minor_snapshot IS NULL AND currency_code_snapshot IS NULL AND line_total_minor IS NULL) OR (unit_price_minor_snapshot IS NOT NULL AND currency_code_snapshot IS NOT NULL AND line_total_minor IS NOT NULL));
