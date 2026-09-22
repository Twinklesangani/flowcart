BEGIN;
-- Refuse a lossy downgrade rather than making paid stock available or forgetting chargeable intents.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM payments WHERE provider_name IS NOT NULL)
       OR EXISTS (SELECT 1 FROM orders WHERE status='paid')
       OR EXISTS (SELECT 1 FROM inventory_reservations WHERE status IN ('payment_held','committed'))
       OR EXISTS (SELECT 1 FROM payment_provider_events) THEN
        RAISE EXCEPTION 'M12 downgrade refused: provider data requires explicit reconciliation/archival';
    END IF;
END $$;
DROP TABLE payment_provider_events;
DROP INDEX payments_provider_id_idx;
DROP INDEX payments_reconcile_idx;
ALTER TABLE payments DROP CONSTRAINT payments_provider_binding_check, DROP CONSTRAINT payments_provider_setup_check,
    DROP COLUMN provider_name, DROP COLUMN provider_payment_id, DROP COLUMN provider_status,
    DROP COLUMN provider_livemode, DROP COLUMN checkout_deadline_at, DROP COLUMN cancel_requested_at,
    DROP COLUMN close_reason, DROP COLUMN next_reconcile_at, DROP COLUMN reconcile_attempts, DROP COLUMN attention_reason;
ALTER TABLE orders DROP CONSTRAINT orders_status_check, DROP COLUMN paid_at, DROP COLUMN cancel_requested_at;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN ('pending','cancelled'));
ALTER TABLE inventory_reservations DROP CONSTRAINT inventory_reservations_status_check;
ALTER TABLE inventory_reservations ADD CONSTRAINT inventory_reservations_status_check CHECK (status IN ('active','released','expired'));
COMMIT;
