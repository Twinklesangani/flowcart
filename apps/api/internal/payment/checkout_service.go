package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flowcart/apps/api/internal/organization"
	"github.com/google/uuid"
	"strings"
	"time"
)

type checkoutRepository interface {
	CreateProvider(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, bool) (Payment, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Payment, error)
	List(context.Context, uuid.UUID, uuid.UUID) ([]Payment, error)
	Prepare(context.Context, uuid.UUID, uuid.UUID) (Payment, error)
	Bind(context.Context, Payment, ProviderIntent) (Payment, error)
	Schedule(context.Context, Payment, string) (Payment, error)
	ConfirmCancelled(context.Context, Payment, ProviderIntent) (Payment, error)
	MarkAttention(context.Context, Payment, string) (Payment, error)
	DuePayments(context.Context) ([]Payment, error)
	AcceptEvent(context.Context, VerifiedEvent) error
	ProcessEvents(context.Context) error
}
type CheckoutService struct {
	repository checkoutRepository
	provider   Provider
	live       bool
}

func NewCheckoutService(r checkoutRepository, p Provider, live bool) *CheckoutService {
	return &CheckoutService{r, p, live}
}
func (s *CheckoutService) Enabled() bool { return s != nil && s.provider != nil }

func (s *CheckoutService) Create(ctx context.Context, tenant organization.TenantContext, orderID uuid.UUID, key string) (CheckoutResult, error) {
	if !canWrite(tenant.Role) {
		return CheckoutResult{}, ErrForbidden
	}
	key = strings.TrimSpace(key)
	if orderID == uuid.Nil || key == "" || len(key) > 200 {
		return CheckoutResult{}, ErrInvalidInput
	}
	if !s.Enabled() {
		return CheckoutResult{}, ErrProviderUnavailable
	}
	hash := sha256.Sum256([]byte(orderID.String()))
	p, err := s.repository.CreateProvider(ctx, tenant.OrganizationID, tenant.UserID, orderID, key, hex.EncodeToString(hash[:]), s.live)
	if err != nil {
		return CheckoutResult{}, err
	}
	if p.ProviderName == nil {
		return CheckoutResult{Payment: p}, nil
	} // Never upgrade an old M11 replay.
	return s.advance(ctx, p, true)
}

func (s *CheckoutService) advance(ctx context.Context, p Payment, credentials bool) (CheckoutResult, error) {
	p, err := s.repository.Prepare(ctx, p.OrganizationID, p.ID)
	if err != nil {
		return CheckoutResult{}, err
	}
	result := CheckoutResult{Payment: p, Processing: p.Status == StatusPending}
	if p.Status != StatusPending || p.ProviderName == nil || p.AttentionReason != nil {
		return result, nil
	}
	if p.ProviderLivemode == nil || *p.ProviderLivemode != s.live {
		p, err = s.repository.MarkAttention(ctx, p, "provider_environment_changed")
		return CheckoutResult{Payment: p, Processing: true}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var intent ProviderIntent
	if p.ProviderPaymentID == nil {
		if p.CheckoutDeadlineAt == nil || time.Since(p.CheckoutDeadlineAt.Add(-CheckoutTTL)) >= InitializationRetryWindow {
			p, err = s.repository.MarkAttention(ctx, p, "initialization_retry_window_exceeded")
			return CheckoutResult{Payment: p, Processing: true}, err
		}
		intent, err = s.provider.CreatePaymentIntent(callCtx, IntentRequest{PaymentID: p.ID, Amount: p.AmountMinor, Currency: p.CurrencyCode, IdempotencyKey: providerKey(p.ID)})
	} else {
		intent, err = s.provider.GetPaymentIntent(callCtx, *p.ProviderPaymentID)
	}
	if err != nil {
		p, scheduleErr := s.repository.Schedule(ctx, p, "provider_request")
		return CheckoutResult{Payment: p, Processing: true}, scheduleErr
	}
	p, err = s.repository.Bind(ctx, p, intent)
	if err != nil {
		return CheckoutResult{}, err
	}
	// Recheck cancellation/cutoff after network latency and binding.
	p, err = s.repository.Prepare(ctx, p.OrganizationID, p.ID)
	if err != nil {
		return CheckoutResult{}, err
	}
	if p.Status != StatusPending || p.AttentionReason != nil {
		return CheckoutResult{Payment: p, Processing: p.Status == StatusPending}, nil
	}
	if intent.Status == "canceled" {
		p, err = s.repository.ConfirmCancelled(ctx, p, intent)
		return CheckoutResult{Payment: p}, err
	}
	if p.CancelRequestedAt != nil && intent.Status != "succeeded" {
		cancelCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		cancelled, cancelErr := s.provider.CancelPaymentIntent(cancelCtx, intent.ID, "flowcart:payment:"+p.ID.String()+":cancel:v1")
		stop()
		if cancelErr == nil && cancelled.Status == "canceled" {
			p, err = s.repository.ConfirmCancelled(ctx, p, cancelled)
			return CheckoutResult{Payment: p}, err
		}
		p, err = s.repository.Schedule(ctx, p, "cancellation_uncertain")
		return CheckoutResult{Payment: p, Processing: true}, err
	}
	// A GET saying succeeded is not a verified event; keep the hold until its signed event arrives.
	p, err = s.repository.Schedule(ctx, p, "awaiting_event")
	if err != nil {
		return CheckoutResult{}, err
	}
	result = CheckoutResult{Payment: p, Processing: true}
	if credentials && p.CancelRequestedAt == nil && p.AttentionReason == nil && p.Status == StatusPending && p.CheckoutDeadlineAt != nil && time.Now().Before(*p.CheckoutDeadlineAt) && (intent.Status == "requires_payment_method" || intent.Status == "requires_confirmation" || intent.Status == "requires_action") {
		result.ClientSecret = intent.ClientSecret
		result.Processing = intent.ClientSecret == ""
	}
	return result, nil
}

func (s *CheckoutService) Receive(ctx context.Context, body []byte, signature string) error {
	if !s.Enabled() {
		return ErrProviderUnavailable
	}
	e, err := s.provider.VerifyWebhook(body, signature)
	if err != nil {
		return ErrInvalidWebhook
	}
	if e.Livemode != s.live {
		return ErrInvalidWebhook
	}
	return s.repository.AcceptEvent(ctx, e)
}
func (s *CheckoutService) Reconcile(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	eventErr := s.repository.ProcessEvents(ctx)
	ps, err := s.repository.DuePayments(ctx)
	if err != nil {
		return errors.Join(eventErr, err)
	}
	for _, p := range ps {
		if _, err := s.advance(ctx, p, false); err != nil {
			eventErr = errors.Join(eventErr, err)
		}
	}
	return errors.Join(eventErr, s.repository.ProcessEvents(ctx))
}
func (s *CheckoutService) ReconcileOrder(ctx context.Context, org, orderID uuid.UUID) error {
	if !s.Enabled() {
		return nil
	} // Local cancellation has already recorded durable intent, keeping holds safe.
	ps, err := s.repository.List(ctx, org, orderID)
	if err != nil {
		return err
	}
	for _, p := range ps {
		if p.Status == StatusPending && p.ProviderName != nil {
			if _, err = s.advance(ctx, p, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// The timer only triggers durable work. Restarting creates no new payment identity.
func (s *CheckoutService) Run(ctx context.Context, onError func()) {
	if !s.Enabled() {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.Reconcile(ctx); err != nil && ctx.Err() == nil {
			onError()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
