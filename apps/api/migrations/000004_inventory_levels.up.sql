CREATE TABLE inventory_levels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
    on_hand_quantity BIGINT NOT NULL DEFAULT 0 CHECK (on_hand_quantity >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, product_id, warehouse_id)
);

CREATE INDEX inventory_levels_organization_id_idx ON inventory_levels (organization_id);
CREATE INDEX inventory_levels_organization_product_idx ON inventory_levels (organization_id, product_id);
CREATE INDEX inventory_levels_organization_warehouse_idx ON inventory_levels (organization_id, warehouse_id);