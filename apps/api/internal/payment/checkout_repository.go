package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type orderState struct {
	Status            string
	CancelRequestedAt *time.Time
}

// All existing aggregate mutations take inventory -> order -> payments -> reservations.
// Discovery reads never lock. Order item/inventory associations are immutable.
func lockAggregate(ctx context.Context, tx pgx.Tx, org, orderID uuid.UUID) (orderState, error) {
	var state orderState
	rows, err := tx.Query(ctx, `SELECT DISTINCT r.inventory_level_id FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE r.organization_id=$1 AND oi.order_id=$2 ORDER BY r.inventory_level_id`, org, orderID)
	if err != nil {
		return state, fmt.Errorf("discover payment inventory: %w", err)
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return state, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return state, err
	}
	for _, id := range ids {
		var locked uuid.UUID
		if err = tx.QueryRow(ctx, `SELECT id FROM inventory_levels WHERE organization_id=$1 AND id=$2 FOR UPDATE`, org, id).Scan(&locked); err != nil {
			return state, fmt.Errorf("lock payment inventory: %w", err)
		}
	}
	err = tx.QueryRow(ctx, `SELECT status,cancel_requested_at FROM orders WHERE organization_id=$1 AND id=$2 FOR UPDATE`, org, orderID).Scan(&state.Status, &state.CancelRequestedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, ErrOrderNotFound
	}
	if err != nil {
		return state, fmt.Errorf("lock payment order: %w", err)
	}
	for _, query := range []string{
		`SELECT id FROM payments WHERE organization_id=$1 AND order_id=$2 ORDER BY id FOR UPDATE`,
		`SELECT r.id FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE r.organization_id=$1 AND oi.order_id=$2 ORDER BY r.id FOR UPDATE OF r`,
	} {
		rows, err = tx.Query(ctx, query, org, orderID)
		if err != nil {
			return state, err
		}
		for rows.Next() {
			var id uuid.UUID
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return state, err
			}
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return state, err
		}
	}
	return state, nil
}

func coverage(ctx context.Context, tx pgx.Tx, org, orderID uuid.UUID, status string) (bool, error) {
	var valid bool
	err := tx.QueryRow(ctx, `SELECT COUNT(*)>0 AND COALESCE(bool_and(covered=quantity),false) FROM (
 SELECT oi.quantity,COALESCE(SUM(r.quantity) FILTER (WHERE r.status=$3 AND ($3<>'active' OR r.expires_at>statement_timestamp())),0) covered
 FROM order_items oi LEFT JOIN inventory_reservations r ON r.organization_id=oi.organization_id AND r.order_item_id=oi.id
 WHERE oi.organization_id=$1 AND oi.order_id=$2 GROUP BY oi.id,oi.quantity) coverage`, org, orderID, status).Scan(&valid)
	return valid, err
}

func (r *Repository) CreateProvider(ctx context.Context, org, user, orderID uuid.UUID, key, hash string, live bool) (Payment, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Payment{}, err
	}
	defer tx.Rollback(ctx)
	// A read-only replay never initializes a providerless M11 payment.
	existing, err := scanPayment(tx.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE organization_id=$1 AND idempotency_key=$2`, org, key))
	if err == nil {
		if existing.OrderID != orderID || existing.RequestHash != hash {
			return Payment{}, ErrIdempotencyKeyReused
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, err
	}
	state, err := lockAggregate(ctx, tx, org, orderID)
	if err != nil {
		return Payment{}, err
	}
	existing, err = scanPayment(tx.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE organization_id=$1 AND idempotency_key=$2`, org, key))
	if err == nil {
		if existing.OrderID != orderID || existing.RequestHash != hash {
			return Payment{}, ErrIdempotencyKeyReused
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, err
	}
	if state.Status == "paid" {
		return Payment{}, ErrPaymentAlreadySucceeded
	}
	if state.Status != "pending" || state.CancelRequestedAt != nil {
		return Payment{}, ErrOrderCancelled
	}
	var pending, succeeded bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payments WHERE organization_id=$1 AND order_id=$2 AND status='pending'),EXISTS(SELECT 1 FROM payments WHERE organization_id=$1 AND order_id=$2 AND status='succeeded')`, org, orderID).Scan(&pending, &succeeded); err != nil {
		return Payment{}, err
	}
	if succeeded {
		return Payment{}, ErrPaymentAlreadySucceeded
	}
	if pending {
		return Payment{}, ErrPaymentInProgress
	}
	var amount *int64
	var currency *string
	if err = tx.QueryRow(ctx, `SELECT subtotal_minor,currency_code FROM orders WHERE organization_id=$1 AND id=$2`, org, orderID).Scan(&amount, &currency); err != nil {
		return Payment{}, err
	}
	if amount == nil || currency == nil || *amount <= 0 {
		return Payment{}, ErrPaymentNotRequired
	}
	valid, err := coverage(ctx, tx, org, orderID, "active")
	if err != nil {
		return Payment{}, err
	}
	if !valid {
		return Payment{}, ErrReservationExpired
	}
	p, err := scanPayment(tx.QueryRow(ctx, `INSERT INTO payments(organization_id,order_id,created_by_user_id,amount_minor,currency_code,idempotency_key,request_hash,provider_name,provider_livemode,checkout_deadline_at,next_reconcile_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,'stripe',$8,statement_timestamp()+INTERVAL '30 minutes',statement_timestamp()) ON CONFLICT(organization_id,idempotency_key) DO NOTHING RETURNING `+paymentColumns, org, orderID, user, *amount, *currency, key, hash, live))
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err = scanPayment(tx.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE organization_id=$1 AND idempotency_key=$2`, org, key))
		if err != nil {
			return Payment{}, err
		}
		if existing.OrderID != orderID || existing.RequestHash != hash {
			return Payment{}, ErrIdempotencyKeyReused
		}
		return existing, nil
	}
	if err != nil {
		return Payment{}, fmt.Errorf("insert provider payment: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE inventory_reservations r SET status='payment_held' FROM order_items oi WHERE r.organization_id=$1 AND oi.organization_id=$1 AND r.order_item_id=oi.id AND oi.order_id=$2 AND r.status='active' AND r.expires_at>statement_timestamp()`, org, orderID); err != nil {
		return Payment{}, err
	}
	// Recheck after the update in case a reservation expired during initialization itself.
	valid, err = coverage(ctx, tx, org, orderID, "payment_held")
	if err != nil {
		return Payment{}, err
	}
	if !valid {
		return Payment{}, ErrReservationExpired
	}
	if err = tx.Commit(ctx); err != nil {
		return Payment{}, err
	}
	return p, nil
}

// mutatePayment reloads state after external calls; stale responses cannot regress terminal states.
func (r *Repository) mutatePayment(ctx context.Context, org, id uuid.UUID, fn func(pgx.Tx, *Payment, orderState) error) (Payment, error) {
	p, err := r.Get(ctx, org, id)
	if err != nil {
		return p, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)
	state, err := lockAggregate(ctx, tx, org, p.OrderID)
	if err != nil {
		return p, err
	}
	p, err = getPaymentTx(ctx, tx, org, id)
	if err != nil {
		return p, err
	}
	if err = fn(tx, &p, state); err != nil {
		return p, err
	}
	p, err = getPaymentTx(ctx, tx, org, id)
	if err != nil {
		return p, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p, err
	}
	return p, nil
}

func attention(ctx context.Context, tx pgx.Tx, p Payment, reason string) error {
	_, err := tx.Exec(ctx, `UPDATE payments SET attention_reason=$3,close_reason='manual_attention',next_reconcile_at=NULL,updated_at=NOW() WHERE organization_id=$1 AND id=$2`, p.OrganizationID, p.ID, reason)
	return err
}
func matches(p Payment, intent ProviderIntent) bool {
	return p.ProviderName != nil && *p.ProviderName == "stripe" && p.ProviderLivemode != nil && *p.ProviderLivemode == intent.Livemode && p.AmountMinor == intent.Amount && p.CurrencyCode == intent.Currency && intent.ID != "" && (p.ProviderPaymentID == nil || *p.ProviderPaymentID == intent.ID)
}

func (r *Repository) Bind(ctx context.Context, p Payment, intent ProviderIntent) (Payment, error) {
	return r.mutatePayment(ctx, p.OrganizationID, p.ID, func(tx pgx.Tx, current *Payment, state orderState) error {
		if !matches(*current, intent) {
			return attention(ctx, tx, *current, "provider_identity_or_amount_mismatch")
		}
		if current.Status != "pending" {
			return nil
		}
		_, err := tx.Exec(ctx, `UPDATE payments SET provider_payment_id=$3,provider_status=$4,updated_at=NOW() WHERE organization_id=$1 AND id=$2`, p.OrganizationID, p.ID, intent.ID, intent.Status)
		return err
	})
}

func (r *Repository) Prepare(ctx context.Context, org, id uuid.UUID) (Payment, error) {
	return r.mutatePayment(ctx, org, id, func(tx pgx.Tx, p *Payment, state orderState) error {
		if p.Status != "pending" || p.ProviderName == nil || p.AttentionReason != nil {
			return nil
		}
		var deadline bool
		if err := tx.QueryRow(ctx, `SELECT checkout_deadline_at<=statement_timestamp() FROM payments WHERE organization_id=$1 AND id=$2`, org, id).Scan(&deadline); err != nil {
			return err
		}
		if deadline || state.CancelRequestedAt != nil {
			reason := "deadline"
			if state.CancelRequestedAt != nil {
				reason = "customer"
			}
			if p.CloseReason != nil && *p.CloseReason == "failure" {
				reason = "failure"
			}
			_, err := tx.Exec(ctx, `UPDATE payments SET cancel_requested_at=COALESCE(cancel_requested_at,NOW()),close_reason=$3 WHERE organization_id=$1 AND id=$2`, org, id, reason)
			return err
		}
		return nil
	})
}

func (r *Repository) Schedule(ctx context.Context, p Payment, reason string) (Payment, error) {
	return r.mutatePayment(ctx, p.OrganizationID, p.ID, func(tx pgx.Tx, current *Payment, _ orderState) error {
		if current.Status != "pending" || current.AttentionReason != nil {
			return nil
		}
		if current.ReconcileAttempts >= 20 {
			return attention(ctx, tx, *current, "retry_exhausted_"+reason)
		}
		_, err := tx.Exec(ctx, `UPDATE payments SET reconcile_attempts=reconcile_attempts+1,next_reconcile_at=statement_timestamp()+ LEAST(1800,30*power(2,LEAST(reconcile_attempts,6))) * INTERVAL '1 second',updated_at=NOW() WHERE organization_id=$1 AND id=$2`, p.OrganizationID, p.ID)
		return err
	})
}

func (r *Repository) ConfirmCancelled(ctx context.Context, p Payment, intent ProviderIntent) (Payment, error) {
	return r.mutatePayment(ctx, p.OrganizationID, p.ID, func(tx pgx.Tx, current *Payment, state orderState) error {
		if !matches(*current, intent) || intent.Status != "canceled" {
			return attention(ctx, tx, *current, "invalid_cancellation_result")
		}
		if current.Status != "pending" || current.AttentionReason != nil {
			return nil
		}
		failed := current.CloseReason != nil && *current.CloseReason == "failure"
		status := "cancelled"
		if failed {
			status = "failed"
		}
		if _, err := tx.Exec(ctx, `UPDATE payments SET status=$3,provider_status='canceled',next_reconcile_at=NULL,updated_at=NOW() WHERE organization_id=$1 AND id=$2`, p.OrganizationID, p.ID, status); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE inventory_reservations r SET status=CASE WHEN expires_at<=statement_timestamp() THEN 'expired' WHEN $3 THEN 'active' ELSE 'released' END,expired_at=CASE WHEN expires_at<=statement_timestamp() THEN NOW() ELSE expired_at END,released_at=CASE WHEN NOT $3 AND expires_at>statement_timestamp() THEN NOW() ELSE released_at END FROM order_items oi WHERE r.organization_id=$1 AND oi.organization_id=$1 AND r.order_item_id=oi.id AND oi.order_id=$2 AND r.status='payment_held'`, p.OrganizationID, p.OrderID, failed); err != nil {
			return err
		}
		if !failed {
			_, err := tx.Exec(ctx, `UPDATE orders SET status='cancelled',cancelled_at=NOW(),updated_at=NOW() WHERE organization_id=$1 AND id=$2 AND status='pending'`, p.OrganizationID, p.OrderID)
			return err
		}
		return nil
	})
}

func (r *Repository) DuePayments(ctx context.Context) ([]Payment, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+paymentColumns+` FROM payments WHERE provider_name='stripe' AND status='pending' AND attention_reason IS NULL AND next_reconcile_at<=statement_timestamp() ORDER BY next_reconcile_at LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ps := []Payment{}
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		ps = append(ps, p)
	}
	return ps, rows.Err()
}
