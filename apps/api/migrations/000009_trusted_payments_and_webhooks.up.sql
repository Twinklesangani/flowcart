BEGIN;
ALTER TABLE orders DROP CONSTRAINT orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN ('pending','cancelled','paid')),
    ADD COLUMN paid_at TIMESTAMPTZ,
    ADD COLUMN cancel_requested_at TIMESTAMPTZ;
ALTER TABLE inventory_reservations DROP CONSTRAINT inventory_reservations_status_check;
ALTER TABLE inventory_reservations ADD CONSTRAINT inventory_reservations_status_check
    CHECK (status IN ('active','released','expired','payment_held','committed'));
ALTER TABLE payments
    ADD COLUMN provider_name TEXT,
    ADD COLUMN provider_payment_id TEXT,
    ADD COLUMN provider_status TEXT,
    ADD COLUMN provider_livemode BOOLEAN,
    ADD COLUMN checkout_deadline_at TIMESTAMPTZ,
    ADD COLUMN cancel_requested_at TIMESTAMPTZ,
    ADD COLUMN close_reason TEXT CHECK (close_reason IN ('customer','deadline','failure','manual_attention')),
    ADD COLUMN next_reconcile_at TIMESTAMPTZ,
    ADD COLUMN reconcile_attempts INTEGER NOT NULL DEFAULT 0 CHECK (reconcile_attempts >= 0),
    ADD COLUMN attention_reason TEXT,
    ADD CONSTRAINT payments_provider_binding_check CHECK (provider_payment_id IS NULL OR provider_name IS NOT NULL),
    ADD CONSTRAINT payments_provider_setup_check CHECK (provider_name IS NULL OR (provider_name='stripe' AND provider_livemode IS NOT NULL AND checkout_deadline_at IS NOT NULL));
CREATE UNIQUE INDEX payments_provider_id_idx ON payments(provider_name,provider_payment_id) WHERE provider_payment_id IS NOT NULL;
CREATE INDEX payments_reconcile_idx ON payments(next_reconcile_at) WHERE status='pending' AND provider_name IS NOT NULL;
CREATE TABLE payment_provider_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_name TEXT NOT NULL,
    provider_event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    provider_payment_id TEXT,
    organization_id UUID,
    payment_id UUID,
    provider_status TEXT NOT NULL,
    amount BIGINT NOT NULL,
    amount_received BIGINT NOT NULL,
    currency TEXT NOT NULL,
    livemode BOOLEAN NOT NULL,
    provider_created_at TIMESTAMPTZ NOT NULL,
    payload_hash TEXT NOT NULL CHECK (length(payload_hash)=64),
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    outcome TEXT NOT NULL DEFAULT 'pending' CHECK (outcome IN ('pending','processed','ignored','quarantined')),
    reason_code TEXT,
    next_attempt_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(provider_name,provider_event_id),
    CHECK ((organization_id IS NULL) = (payment_id IS NULL)),
    FOREIGN KEY (organization_id,payment_id) REFERENCES payments(organization_id,id) ON DELETE CASCADE
);
CREATE INDEX payment_provider_events_pending_idx ON payment_provider_events(next_attempt_at) WHERE outcome='pending';
COMMIT;
