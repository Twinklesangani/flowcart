package payment

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
)

const CheckoutTTL = 30 * time.Minute
const InitializationRetryWindow = 23 * time.Hour

var ErrProviderUnavailable = errors.New("payment provider unavailable")
var ErrInvalidWebhook = errors.New("invalid provider webhook")
var ErrProviderRequest = errors.New("provider request failed")
var ErrCheckoutClosed = errors.New("checkout closed")

// No SDK types cross this boundary. ClientSecret is transient and never part of a persisted Payment.
type IntentRequest struct {
	PaymentID                uuid.UUID
	Amount                   int64
	Currency, IdempotencyKey string
}
type ProviderIntent struct {
	ID, Status, Currency   string
	Amount, AmountReceived int64
	Livemode               bool
	ClientSecret           string `json:"-"`
}
type VerifiedEvent struct {
	ID, Type, Provider, IntentID, Status, Currency, PayloadHash string
	Amount, AmountReceived                                      int64
	Livemode                                                    bool
	CreatedAt                                                   time.Time
}
type Provider interface {
	CreatePaymentIntent(context.Context, IntentRequest) (ProviderIntent, error)
	GetPaymentIntent(context.Context, string) (ProviderIntent, error)
	CancelPaymentIntent(context.Context, string, string) (ProviderIntent, error)
	VerifyWebhook([]byte, string) (VerifiedEvent, error)
}
type CheckoutResult struct {
	Payment
	ClientSecret string `json:"client_secret,omitempty"`
	Processing   bool   `json:"processing"`
}

func providerKey(id uuid.UUID) string { return "flowcart:payment:" + id.String() + ":create:v1" }
