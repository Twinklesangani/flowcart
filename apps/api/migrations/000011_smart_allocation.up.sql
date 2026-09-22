ALTER TABLE orders
    ADD COLUMN allocation_method TEXT NOT NULL DEFAULT 'manual',
    ADD COLUMN allocation_strategy TEXT,
    ADD CONSTRAINT orders_allocation_metadata_chk CHECK (
        (allocation_method = 'manual' AND allocation_strategy IS NULL)
        OR
        (allocation_method = 'automatic' AND allocation_strategy = 'minimize_splits_v1')
    );