CREATE TABLE payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL,
    order_id UUID NOT NULL,
    created_by_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'failed', 'succeeded', 'cancelled')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    currency_code VARCHAR(3) NOT NULL CHECK (currency_code ~ '^[A-Z]{3}$'),
    idempotency_key VARCHAR(200) NOT NULL,
    request_hash VARCHAR(64) NOT NULL CHECK (char_length(request_hash) = 64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, id),
    UNIQUE (organization_id, idempotency_key),
    CONSTRAINT payments_order_fk FOREIGN KEY (organization_id, order_id)
        REFERENCES orders (organization_id, id) ON DELETE CASCADE
);

CREATE INDEX payments_organization_order_idx ON payments (organization_id, order_id, created_at DESC);
CREATE UNIQUE INDEX payments_one_pending_per_order_idx ON payments (organization_id, order_id) WHERE status = 'pending';
CREATE UNIQUE INDEX payments_one_succeeded_per_order_idx ON payments (organization_id, order_id) WHERE status = 'succeeded';
