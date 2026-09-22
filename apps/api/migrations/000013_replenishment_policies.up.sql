BEGIN;

ALTER TABLE inventory_levels
    ADD COLUMN reorder_point BIGINT,
    ADD COLUMN target_stock_level BIGINT,
    ADD CONSTRAINT inventory_levels_replenishment_policy_chk CHECK (
        (reorder_point IS NULL AND target_stock_level IS NULL)
        OR
        (reorder_point IS NOT NULL AND target_stock_level IS NOT NULL
            AND reorder_point >= 0
            AND target_stock_level >= reorder_point)
    );

CREATE INDEX inventory_levels_replenishment_policy_idx
    ON inventory_levels (organization_id, product_id, warehouse_id)
    WHERE reorder_point IS NOT NULL;

COMMIT;
