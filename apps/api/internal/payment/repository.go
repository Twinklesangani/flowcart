package payment

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"flowcart/apps/api/internal/audit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const paymentColumns = `id, organization_id, order_id, created_by_user_id, status, amount_minor, currency_code, idempotency_key, request_hash, created_at, updated_at, provider_name,provider_payment_id,provider_status,provider_livemode,checkout_deadline_at,cancel_requested_at,close_reason,next_reconcile_at,reconcile_attempts,attention_reason`

func scanPayment(row pgx.Row) (Payment, error) {
	var item Payment
	err := row.Scan(&item.ID, &item.OrganizationID, &item.OrderID, &item.CreatedByUserID, &item.Status, &item.AmountMinor, &item.CurrencyCode, &item.IdempotencyKey, &item.RequestHash, &item.CreatedAt, &item.UpdatedAt, &item.ProviderName, &item.ProviderPaymentID, &item.ProviderStatus, &item.ProviderLivemode, &item.CheckoutDeadlineAt, &item.CancelRequestedAt, &item.CloseReason, &item.NextReconcileAt, &item.ReconcileAttempts, &item.AttentionReason)
	return item, err
}

func (r *Repository) Create(ctx context.Context, organizationID, userID, orderID uuid.UUID, key, requestHash string) (Payment, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Payment{}, fmt.Errorf("begin payment: %w", err)
	}
	defer tx.Rollback(ctx)
	var existingID uuid.UUID
	var existingOrder uuid.UUID
	var existingHash string
	err = tx.QueryRow(ctx, `SELECT id, order_id, request_hash FROM payments WHERE organization_id=$1 AND idempotency_key=$2 FOR UPDATE`, organizationID, key).Scan(&existingID, &existingOrder, &existingHash)
	if err == nil {
		if existingHash != requestHash || existingOrder != orderID {
			return Payment{}, ErrIdempotencyKeyReused
		}
		item, readErr := getPaymentTx(ctx, tx, organizationID, existingID)
		if readErr != nil {
			return Payment{}, readErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return Payment{}, fmt.Errorf("commit payment replay: %w", commitErr)
		}
		return item, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, fmt.Errorf("find payment idempotency key: %w", err)
	}

	rows, err := tx.Query(ctx, `SELECT DISTINCT r.inventory_level_id FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE r.organization_id=$1 AND oi.order_id=$2`, organizationID, orderID)
	if err != nil {
		return Payment{}, fmt.Errorf("find payment inventory: %w", err)
	}
	var inventoryIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if scanErr := rows.Scan(&id); scanErr != nil {
			rows.Close()
			return Payment{}, scanErr
		}
		inventoryIDs = append(inventoryIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Payment{}, err
	}
	sort.Slice(inventoryIDs, func(i, j int) bool { return inventoryIDs[i].String() < inventoryIDs[j].String() })
	for _, inventoryID := range inventoryIDs {
		var locked uuid.UUID
		lockErr := tx.QueryRow(ctx, `SELECT id FROM inventory_levels WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, inventoryID).Scan(&locked)
		if errors.Is(lockErr, pgx.ErrNoRows) {
			return Payment{}, ErrOrderNotFound
		}
		if lockErr != nil {
			return Payment{}, fmt.Errorf("lock payment inventory: %w", lockErr)
		}
	}
	var status string
	var currency *string
	var subtotal *int64
	err = tx.QueryRow(ctx, `SELECT status, currency_code, subtotal_minor FROM orders WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, orderID).Scan(&status, &currency, &subtotal)
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrOrderNotFound
	}
	if err != nil {
		return Payment{}, fmt.Errorf("lock payment order: %w", err)
	}
	var lockedID uuid.UUID
	var lockedOrder uuid.UUID
	var lockedHash string
	lockReplayErr := tx.QueryRow(ctx, `SELECT id, order_id, request_hash FROM payments WHERE organization_id=$1 AND idempotency_key=$2 FOR UPDATE`, organizationID, key).Scan(&lockedID, &lockedOrder, &lockedHash)
	if lockReplayErr == nil {
		if lockedOrder != orderID || lockedHash != requestHash {
			return Payment{}, ErrIdempotencyKeyReused
		}
		replay, readErr := getPaymentTx(ctx, tx, organizationID, lockedID)
		if readErr != nil {
			return Payment{}, readErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return Payment{}, fmt.Errorf("commit payment replay: %w", commitErr)
		}
		return replay, nil
	}
	if !errors.Is(lockReplayErr, pgx.ErrNoRows) {
		return Payment{}, fmt.Errorf("reload payment idempotency key: %w", lockReplayErr)
	}
	if status == "cancelled" {
		return Payment{}, ErrOrderCancelled
	}
	if currency == nil || subtotal == nil {
		return Payment{}, ErrPaymentNotRequired
	}
	if *subtotal == 0 {
		return Payment{}, ErrPaymentNotRequired
	}
	coverageRows, err := tx.Query(ctx, `SELECT oi.quantity, COALESCE(SUM(r.quantity) FILTER (WHERE r.status='active' AND r.expires_at > statement_timestamp()),0) FROM order_items oi LEFT JOIN inventory_reservations r ON r.organization_id=oi.organization_id AND r.order_item_id=oi.id WHERE oi.organization_id=$1 AND oi.order_id=$2 GROUP BY oi.id,oi.quantity`, organizationID, orderID)
	if err != nil {
		return Payment{}, fmt.Errorf("check payment reservation coverage: %w", err)
	}
	coverageCount := 0
	for coverageRows.Next() {
		var required, reserved int64
		if scanErr := coverageRows.Scan(&required, &reserved); scanErr != nil {
			coverageRows.Close()
			return Payment{}, scanErr
		}
		coverageCount++
		if reserved < required {
			coverageRows.Close()
			return Payment{}, ErrReservationExpired
		}
	}
	if rowsErr := coverageRows.Err(); rowsErr != nil {
		coverageRows.Close()
		return Payment{}, rowsErr
	}
	coverageRows.Close()
	if coverageCount == 0 {
		return Payment{}, ErrOrderNotFound
	}
	var pending, succeeded bool
	paymentRows, err := tx.Query(ctx, `SELECT status FROM payments WHERE organization_id=$1 AND order_id=$2 FOR UPDATE`, organizationID, orderID)
	if err != nil {
		return Payment{}, fmt.Errorf("lock order payments: %w", err)
	}
	for paymentRows.Next() {
		var paymentStatus string
		if scanErr := paymentRows.Scan(&paymentStatus); scanErr != nil {
			paymentRows.Close()
			return Payment{}, scanErr
		}
		if paymentStatus == StatusPending {
			pending = true
		}
		if paymentStatus == StatusSucceeded {
			succeeded = true
		}
	}
	if rowsErr := paymentRows.Err(); rowsErr != nil {
		paymentRows.Close()
		return Payment{}, rowsErr
	}
	paymentRows.Close()
	if succeeded {
		return Payment{}, ErrPaymentAlreadySucceeded
	}
	if pending {
		return Payment{}, ErrPaymentInProgress
	}
	item, err := scanPayment(tx.QueryRow(ctx, `INSERT INTO payments (organization_id,order_id,created_by_user_id,status,amount_minor,currency_code,idempotency_key,request_hash) VALUES ($1,$2,$3,'pending',$4,$5,$6,$7) ON CONFLICT (organization_id,idempotency_key) DO NOTHING RETURNING `+paymentColumns, organizationID, orderID, userID, *subtotal, *currency, key, requestHash))
	if errors.Is(err, pgx.ErrNoRows) {
		var replayID uuid.UUID
		var replayOrder uuid.UUID
		var replayHash string
		if reloadErr := tx.QueryRow(ctx, `SELECT id,order_id,request_hash FROM payments WHERE organization_id=$1 AND idempotency_key=$2`, organizationID, key).Scan(&replayID, &replayOrder, &replayHash); reloadErr != nil {
			return Payment{}, fmt.Errorf("reload payment idempotency key: %w", reloadErr)
		}
		if replayOrder != orderID || replayHash != requestHash {
			return Payment{}, ErrIdempotencyKeyReused
		}
		item, err = getPaymentTx(ctx, tx, organizationID, replayID)
		if err != nil {
			return Payment{}, err
		}
	} else if err != nil {
		return Payment{}, fmt.Errorf("create payment: %w", err)
	}
	writer := audit.Writer{}
	if err := writer.AppendEvent(ctx, tx, audit.Event{
		OrganizationID: organizationID,
		EventType:      "payment.created",
		ResourceType:   "payment",
		ResourceID:     item.ID,
		ActorType:      "user",
		ActorUserID:    &userID,
		SourceType:     "payment",
		SourceID:       item.ID,
		Metadata:       map[string]any{"order_id": orderID.String(), "amount_minor": item.AmountMinor, "currency": item.CurrencyCode},
	}); err != nil {
		return Payment{}, fmt.Errorf("append payment.created audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Payment{}, fmt.Errorf("commit payment: %w", err)
	}
	return item, nil
}
func (r *Repository) List(ctx context.Context, organizationID, orderID uuid.UUID) ([]Payment, error) {
	var existingOrder uuid.UUID
	if err := r.pool.QueryRow(ctx, `SELECT id FROM orders WHERE organization_id=$1 AND id=$2`, organizationID, orderID).Scan(&existingOrder); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOrderNotFound
	} else if err != nil {
		return nil, fmt.Errorf("find payment order: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT `+paymentColumns+` FROM payments WHERE organization_id=$1 AND order_id=$2 ORDER BY created_at`, organizationID, orderID)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	defer rows.Close()
	items := make([]Payment, 0)
	for rows.Next() {
		item, scanErr := scanPayment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
func (r *Repository) Get(ctx context.Context, organizationID, paymentID uuid.UUID) (Payment, error) {
	item, err := scanPayment(r.pool.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE organization_id=$1 AND id=$2`, organizationID, paymentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrPaymentNotFound
	}
	if err != nil {
		return Payment{}, fmt.Errorf("get payment: %w", err)
	}
	return item, nil
}
func getPaymentTx(ctx context.Context, tx pgx.Tx, organizationID, paymentID uuid.UUID) (Payment, error) {
	item, err := scanPayment(tx.QueryRow(ctx, `SELECT `+paymentColumns+` FROM payments WHERE organization_id=$1 AND id=$2`, organizationID, paymentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrPaymentNotFound
	}
	if err != nil {
		return Payment{}, fmt.Errorf("read payment: %w", err)
	}
	return item, nil
}
func isUniqueError(err error) bool {
	var e *pgconn.PgError
	return errors.As(err, &e) && e.Code == "23505"
}
