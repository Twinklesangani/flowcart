CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'cancelled')),
    idempotency_key VARCHAR(200) NOT NULL,
    request_hash VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cancelled_at TIMESTAMPTZ,
    CHECK (char_length(request_hash) = 64),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, idempotency_key)
);

ALTER TABLE products
    ADD CONSTRAINT products_organization_id_id_key UNIQUE (organization_id, id);

CREATE TABLE order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    order_id UUID NOT NULL,
    product_id UUID NOT NULL,
    sku_snapshot VARCHAR(100) NOT NULL,
    product_name_snapshot VARCHAR(255) NOT NULL,
    quantity BIGINT NOT NULL CHECK (quantity > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, order_id, product_id),
    CONSTRAINT order_items_order_fk FOREIGN KEY (organization_id, order_id)
        REFERENCES orders (organization_id, id) ON DELETE CASCADE,
    CONSTRAINT order_items_product_fk FOREIGN KEY (organization_id, product_id)
        REFERENCES products (organization_id, id) ON DELETE RESTRICT
);

ALTER TABLE inventory_reservations
    ADD COLUMN order_item_id UUID,
    ADD CONSTRAINT inventory_reservations_order_item_fk
        FOREIGN KEY (organization_id, order_item_id)
        REFERENCES order_items (organization_id, id) ON DELETE RESTRICT;

CREATE INDEX orders_organization_created_idx ON orders (organization_id, created_at DESC);
CREATE INDEX order_items_organization_order_idx ON order_items (organization_id, order_id);
CREATE INDEX inventory_reservations_order_item_idx ON inventory_reservations (organization_id, order_item_id);