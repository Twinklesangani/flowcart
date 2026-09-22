package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"flowcart/apps/api/internal/audit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) MarkAttention(ctx context.Context, p Payment, reason string) (Payment, error) {
	return r.mutatePayment(ctx, p.OrganizationID, p.ID, func(tx pgx.Tx, p *Payment, _ orderState) error { return attention(ctx, tx, *p, reason) })
}

func (r *Repository) AcceptEvent(ctx context.Context, e VerifiedEvent) error {
	if e.Provider != "stripe" || e.ID == "" || len(e.PayloadHash) != 64 {
		return ErrInvalidWebhook
	}
	var providerPaymentID any = e.IntentID
	if e.IntentID == "" {
		providerPaymentID = nil
	}
	outcome := "pending"
	reason := ""
	var processedAt any
	if e.Type != "payment_intent.succeeded" && e.Type != "payment_intent.payment_failed" && e.Type != "payment_intent.canceled" {
		outcome = "ignored"
		reason = "unsupported_event"
		processedAt = time.Now()
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO payment_provider_events(provider_name,provider_event_id,event_type,provider_payment_id,provider_status,amount,amount_received,currency,livemode,provider_created_at,payload_hash,outcome,reason_code,processed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,''),$14) ON CONFLICT(provider_name,provider_event_id) DO NOTHING`, e.Provider, e.ID, e.Type, providerPaymentID, e.Status, e.Amount, e.AmountReceived, e.Currency, e.Livemode, e.CreatedAt, e.PayloadHash, outcome, reason, processedAt)
	if err != nil {
		return fmt.Errorf("persist verified provider event: %w", err)
	}
	return nil
}
func (r *Repository) ProcessEvents(ctx context.Context) error {
	rows, err := r.pool.Query(ctx, `SELECT id FROM payment_provider_events WHERE outcome='pending' AND next_attempt_at<=statement_timestamp() ORDER BY received_at,id LIMIT 50`)
	if err != nil {
		return err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	var result error
	for _, id := range ids {
		result = errors.Join(result, r.ProcessEvent(ctx, id))
	}
	return result
}

func (r *Repository) ProcessEvent(ctx context.Context, id uuid.UUID) error {
	var org, paymentID uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT p.organization_id,p.id FROM payment_provider_events e JOIN payments p ON p.provider_name=e.provider_name AND p.provider_payment_id=e.provider_payment_id WHERE e.id=$1`, id).Scan(&org, &paymentID)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = r.pool.Exec(ctx, `UPDATE payment_provider_events SET next_attempt_at=statement_timestamp()+INTERVAL '30 seconds',reason_code='binding_unresolved',outcome=CASE WHEN received_at<statement_timestamp()-INTERVAL '3 days' THEN 'quarantined' ELSE outcome END WHERE id=$1 AND outcome='pending'`, id)
		return err
	}
	if err != nil {
		return err
	}
	_, err = r.mutatePayment(ctx, org, paymentID, func(tx pgx.Tx, p *Payment, state orderState) error {
		var e VerifiedEvent
		var outcome string
		err := tx.QueryRow(ctx, `SELECT provider_name,provider_event_id,event_type,provider_payment_id,provider_status,amount,amount_received,currency,livemode,outcome FROM payment_provider_events WHERE id=$1 FOR UPDATE`, id).Scan(&e.Provider, &e.ID, &e.Type, &e.IntentID, &e.Status, &e.Amount, &e.AmountReceived, &e.Currency, &e.Livemode, &outcome)
		if err != nil {
			return err
		}
		if outcome != "pending" {
			return nil
		}
		finish := func(outcome, reason string) error {
			_, err := tx.Exec(ctx, `UPDATE payment_provider_events SET organization_id=$2,payment_id=$3,outcome=$4,reason_code=NULLIF($5,''),processed_at=NOW(),next_attempt_at=NULL WHERE id=$1`, id, org, paymentID, outcome, reason)
			return err
		}
		quarantine := func(reason string) error {
			if err := attention(ctx, tx, *p, reason); err != nil {
				return err
			}
			return finish("quarantined", reason)
		}
		if e.Provider != "stripe" || p.ProviderPaymentID == nil || *p.ProviderPaymentID != e.IntentID || p.ProviderLivemode == nil || *p.ProviderLivemode != e.Livemode {
			return quarantine("provider_identity_or_environment_mismatch")
		}
		if e.Amount != p.AmountMinor || e.Currency != p.CurrencyCode {
			return quarantine("amount_or_currency_mismatch")
		}
		if e.Type == "payment_intent.succeeded" && (e.AmountReceived != p.AmountMinor || e.Status != "succeeded") {
			return quarantine("success_amount_received_or_status_mismatch")
		}
		if p.Status == StatusSucceeded {
			return finish("ignored", "already_succeeded")
		}
		if p.Status == StatusCancelled || p.Status == StatusFailed {
			if e.Type == "payment_intent.succeeded" {
				return quarantine("success_after_terminal_cancellation")
			}
			return finish("ignored", "terminal_payment")
		}
		if p.AttentionReason != nil {
			return finish("quarantined", "payment_requires_attention")
		}
		switch e.Type {
		case "payment_intent.succeeded":
			if state.Status != "pending" {
				return quarantine("order_not_pending")
			}
			valid, err := coverage(ctx, tx, org, p.OrderID, "payment_held")
			if err != nil {
				return err
			}
			if !valid {
				return quarantine("payment_hold_incomplete")
			}
			if _, err = tx.Exec(ctx, `UPDATE payments SET status='succeeded',provider_status='succeeded',next_reconcile_at=NULL,updated_at=NOW() WHERE organization_id=$1 AND id=$2`, org, p.ID); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE orders SET status='paid',paid_at=NOW(),updated_at=NOW() WHERE organization_id=$1 AND id=$2`, org, p.OrderID); err != nil {
				return err
			}
			writer := audit.Writer{}
			if err := writer.AppendEvent(ctx, tx, audit.Event{
				OrganizationID: org,
				EventType:      "payment.succeeded",
				ResourceType:   "payment",
				ResourceID:     p.ID,
				ActorType:      "provider",
				SourceType:     "payment",
				SourceID:       p.ID,
				Metadata:       map[string]any{"order_id": p.OrderID.String(), "status": "succeeded", "amount_minor": p.AmountMinor, "currency": p.CurrencyCode},
			}); err != nil {
				return err
			}
			if err := writer.AppendEvent(ctx, tx, audit.Event{
				OrganizationID:     org,
				EventType:          "order.paid",
				ResourceType:       "order",
				ResourceID:         p.OrderID,
				ParentResourceType: func() *string { s := "payment"; return &s }(),
				ParentResourceID:   &p.ID,
				ActorType:          "provider",
				SourceType:         "payment",
				SourceID:           p.ID,
				Metadata:           map[string]any{"payment_id": p.ID.String(), "status": "paid"},
			}); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE inventory_reservations r SET status='committed' FROM order_items oi WHERE r.organization_id=$1 AND oi.organization_id=$1 AND r.order_item_id=oi.id AND oi.order_id=$2 AND r.status='payment_held'`, org, p.OrderID); err != nil {
				return err
			}
		case "payment_intent.payment_failed":
			if e.Status != "requires_payment_method" {
				return quarantine("failure_status_mismatch")
			}
			if _, err = tx.Exec(ctx, `UPDATE payments SET provider_status=$3,cancel_requested_at=COALESCE(cancel_requested_at,NOW()),close_reason=COALESCE(close_reason,'failure'),next_reconcile_at=NOW(),updated_at=NOW() WHERE organization_id=$1 AND id=$2`, org, p.ID, e.Status); err != nil {
				return err
			}
		case "payment_intent.canceled":
			if e.Status != "canceled" {
				return quarantine("cancellation_status_mismatch")
			}
			// Reconciliation confirms the terminal provider state outside the transaction.
			if _, err = tx.Exec(ctx, `UPDATE payments SET provider_status='canceled',cancel_requested_at=COALESCE(cancel_requested_at,NOW()),close_reason=COALESCE(close_reason,'customer'),next_reconcile_at=NOW() WHERE organization_id=$1 AND id=$2`, org, p.ID); err != nil {
				return err
			}
		default:
			return finish("ignored", "unsupported_event")
		}
		return finish("processed", "")
	})
	return err
}
