package payment

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending   = "pending"
	StatusFailed    = "failed"
	StatusSucceeded = "succeeded"
	StatusCancelled = "cancelled"
)

type Payment struct {
	ProviderName       *string    `json:"provider_name,omitempty"`
	ProviderPaymentID  *string    `json:"provider_payment_id,omitempty"`
	ProviderStatus     *string    `json:"provider_status,omitempty"`
	ProviderLivemode   *bool      `json:"provider_livemode,omitempty"`
	CheckoutDeadlineAt *time.Time `json:"checkout_deadline_at,omitempty"`
	CancelRequestedAt  *time.Time `json:"cancel_requested_at,omitempty"`
	CloseReason        *string    `json:"close_reason,omitempty"`
	NextReconcileAt    *time.Time `json:"next_reconcile_at,omitempty"`
	ReconcileAttempts  int        `json:"reconcile_attempts"`
	AttentionReason    *string    `json:"attention_reason,omitempty"`
	ID                 uuid.UUID  `json:"id"`
	OrganizationID     uuid.UUID  `json:"organization_id"`
	OrderID            uuid.UUID  `json:"order_id"`
	CreatedByUserID    uuid.UUID  `json:"created_by_user_id"`
	Status             string     `json:"status"`
	AmountMinor        int64      `json:"amount_minor"`
	CurrencyCode       string     `json:"currency_code"`
	IdempotencyKey     string     `json:"idempotency_key"`
	RequestHash        string     `json:"request_hash"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

var (
	ErrInvalidInput            = errors.New("invalid payment input")
	ErrForbidden               = errors.New("forbidden")
	ErrOrderNotFound           = errors.New("order not found")
	ErrPaymentNotFound         = errors.New("payment not found")
	ErrOrderCancelled          = errors.New("order cancelled")
	ErrReservationExpired      = errors.New("reservation expired")
	ErrPaymentInProgress       = errors.New("payment in progress")
	ErrPaymentAlreadySucceeded = errors.New("payment already succeeded")
	ErrPaymentNotRequired      = errors.New("payment not required")
	ErrIdempotencyKeyReused    = errors.New("idempotency key reused")
)
