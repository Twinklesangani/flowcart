ALTER TABLE inventory_levels
    ADD CONSTRAINT inventory_levels_organization_id_id_key UNIQUE (organization_id, id);

CREATE TABLE inventory_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    inventory_level_id UUID NOT NULL,
    quantity BIGINT NOT NULL CHECK (quantity > 0),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'released', 'expired')),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at TIMESTAMPTZ,
    expired_at TIMESTAMPTZ,
    CONSTRAINT inventory_reservations_inventory_fk
        FOREIGN KEY (organization_id, inventory_level_id)
        REFERENCES inventory_levels (organization_id, id)
        ON DELETE CASCADE
);

CREATE INDEX inventory_reservations_organization_inventory_status_idx
    ON inventory_reservations (organization_id, inventory_level_id, status);
CREATE INDEX inventory_reservations_organization_id_idx
    ON inventory_reservations (organization_id, id);