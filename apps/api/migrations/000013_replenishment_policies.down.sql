BEGIN;

DROP INDEX IF EXISTS inventory_levels_replenishment_policy_idx;
ALTER TABLE inventory_levels
    DROP CONSTRAINT IF EXISTS inventory_levels_replenishment_policy_chk,
    DROP COLUMN IF EXISTS target_stock_level,
    DROP COLUMN IF EXISTS reorder_point;

COMMIT;
