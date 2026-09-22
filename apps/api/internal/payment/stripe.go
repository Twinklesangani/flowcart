package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/paymentintent"
)

type StripeProvider struct {
	webhookSecret string
}

func NewStripeProvider(secretKey, webhookSecret string) (*StripeProvider, error) {
	if strings.TrimSpace(secretKey) == "" || strings.TrimSpace(webhookSecret) == "" {
		return nil, ErrProviderUnavailable
	}
	stripe.Key = secretKey
	return &StripeProvider{webhookSecret: webhookSecret}, nil
}

func stripeIntent(intent *stripe.PaymentIntent) ProviderIntent {
	return ProviderIntent{
		ID: intent.ID, Status: string(intent.Status), Currency: strings.ToUpper(string(intent.Currency)),
		Amount: intent.Amount, AmountReceived: intent.AmountReceived, Livemode: intent.Livemode,
		ClientSecret: intent.ClientSecret,
	}
}

func (p *StripeProvider) CreatePaymentIntent(ctx context.Context, request IntentRequest) (ProviderIntent, error) {
	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(request.Amount),
		Currency: stripe.String(strings.ToLower(request.Currency)),
		Metadata: map[string]string{"flowcart_payment_id": request.PaymentID.String()},
	}
	params.Context = ctx
	params.SetIdempotencyKey(request.IdempotencyKey)
	intent, err := paymentintent.New(params)
	if err != nil {
		return ProviderIntent{}, ErrProviderRequest
	}
	return stripeIntent(intent), nil
}

func (p *StripeProvider) GetPaymentIntent(ctx context.Context, id string) (ProviderIntent, error) {
	intent, err := paymentintent.Get(id, &stripe.PaymentIntentParams{Params: stripe.Params{Context: ctx}})
	if err != nil {
		return ProviderIntent{}, ErrProviderRequest
	}
	return stripeIntent(intent), nil
}

func (p *StripeProvider) CancelPaymentIntent(ctx context.Context, id, idempotencyKey string) (ProviderIntent, error) {
	params := &stripe.PaymentIntentCancelParams{Params: stripe.Params{Context: ctx}}
	params.SetIdempotencyKey(idempotencyKey)
	intent, err := paymentintent.Cancel(id, params)
	if err != nil {
		return ProviderIntent{}, ErrProviderRequest
	}
	return stripeIntent(intent), nil
}

func (p *StripeProvider) VerifyWebhook(body []byte, signature string) (VerifiedEvent, error) {
	event, err := stripe.ConstructEvent(body, signature, p.webhookSecret, stripe.WithTolerance(5*time.Minute))
	if err != nil {
		return VerifiedEvent{}, ErrInvalidWebhook
	}
	var intent stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &intent); err != nil || event.ID == "" || intent.ID == "" {
		return VerifiedEvent{}, ErrInvalidWebhook
	}
	hash := sha256.Sum256(body)
	return VerifiedEvent{
		ID: event.ID, Type: string(event.Type), Provider: "stripe", IntentID: intent.ID,
		Status: string(intent.Status), Currency: strings.ToUpper(string(intent.Currency)),
		Amount: intent.Amount, AmountReceived: intent.AmountReceived, Livemode: intent.Livemode,
		PayloadHash: hex.EncodeToString(hash[:]), CreatedAt: time.Unix(event.Created, 0).UTC(),
	}, nil
}

var _ Provider = (*StripeProvider)(nil)
