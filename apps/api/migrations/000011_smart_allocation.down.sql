ALTER TABLE orders
    DROP CONSTRAINT orders_allocation_metadata_chk,
    DROP COLUMN allocation_strategy,
    DROP COLUMN allocation_method;