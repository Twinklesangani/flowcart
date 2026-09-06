DROP TABLE IF EXISTS inventory_reservations;

ALTER TABLE inventory_levels
    DROP CONSTRAINT IF EXISTS inventory_levels_organization_id_id_key;