package payment_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/payment"
	"github.com/google/uuid"
)

func TestReconciliationRestartResolvesProviderEventFromPostgreSQL(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	first := payment.NewRepository(pool)
	created, err := first.CreateProvider(ctx, f.org, f.user, ord.ID, "restart-event", strings.Repeat("9", 64), false)
	if err != nil {
		t.Fatal(err)
	}
	providerID := "pi_restart_event"
	event := payment.VerifiedEvent{ID: "evt_restart_event", Type: "payment_intent.succeeded", Provider: "stripe", IntentID: providerID, Status: "succeeded", Currency: "AUD", Amount: 2000, AmountReceived: 2000, Livemode: false, PayloadHash: strings.Repeat("a", 64), CreatedAt: time.Now()}
	if err := first.AcceptEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := first.ProcessEvents(ctx); err != nil {
		t.Fatal(err)
	}
	var outcome string
	if err := pool.QueryRow(ctx, `SELECT outcome FROM payment_provider_events WHERE provider_event_id=$1`, event.ID).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != "pending" {
		t.Fatalf("first reconciliation outcome=%s", outcome)
	}
	if _, err := pool.Exec(ctx, `UPDATE payments SET provider_payment_id=$2,provider_status='requires_payment_method',next_reconcile_at=NULL WHERE organization_id=$1 AND id=$3`, f.org, providerID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE payment_provider_events SET next_attempt_at=NOW() WHERE provider_event_id=$1`, event.ID); err != nil {
		t.Fatal(err)
	}
	second := payment.NewRepository(pool)
	if err := second.ProcessEvents(ctx); err != nil {
		t.Fatal(err)
	}
	assertPaymentOrderReservation(t, pool, f.org, created.ID, ord.ID, f.inventory, "succeeded", "paid", "committed")
	if err := pool.QueryRow(ctx, `SELECT outcome FROM payment_provider_events WHERE provider_event_id=$1`, event.ID).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != "processed" {
		t.Fatalf("restarted reconciliation outcome=%s", outcome)
	}
}

func TestReconciliationRestartResumesProviderCancellation(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	repo := payment.NewRepository(pool)
	created, err := repo.CreateProvider(ctx, f.org, f.user, ord.ID, "restart-cancel", strings.Repeat("b", 64), false)
	if err != nil {
		t.Fatal(err)
	}
	providerID := "pi_restart_cancel"
	if _, err := pool.Exec(ctx, `UPDATE payments SET provider_payment_id=$2,provider_status='requires_action',cancel_requested_at=NOW(),close_reason='customer',next_reconcile_at=NOW() WHERE organization_id=$1 AND id=$3`, f.org, providerID, created.ID); err != nil {
		t.Fatal(err)
	}
	shared := &restartProvider{intents: map[string]payment.ProviderIntent{providerID: {ID: providerID, Status: "requires_action", Amount: 2000, Currency: "AUD", Livemode: false}}}
	service := payment.NewCheckoutService(repo, shared, false)
	if err := service.ReconcileOrder(ctx, f.org, ord.ID); err != nil {
		t.Fatal(err)
	}
	assertPaymentOrderReservation(t, pool, f.org, created.ID, ord.ID, f.inventory, "cancelled", "cancelled", "released")
	newRepo := payment.NewRepository(pool)
	newService := payment.NewCheckoutService(newRepo, shared, false)
	if err := newService.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	assertPaymentOrderReservation(t, pool, f.org, created.ID, ord.ID, f.inventory, "cancelled", "cancelled", "released")
}

func TestPaymentProviderTenantIsolationAndUnexpectedErrors(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	orgA := paymentFixture(t, pool, 2000)
	orgB := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, orgA.org); cleanupPaymentFixture(t, pool, orgB.org) })
	orderA := createPaymentOrder(t, pool, orgA, 1)
	repo := payment.NewRepository(pool)
	created, err := repo.Create(ctx, orgA.org, orgA.user, orderA.ID, "tenant-payment", strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, orgB.org, created.ID); !errors.Is(err, payment.ErrPaymentNotFound) {
		t.Fatalf("cross-tenant payment get=%v", err)
	}
	if _, err := repo.List(ctx, orgB.org, orderA.ID); !errors.Is(err, payment.ErrOrderNotFound) {
		t.Fatalf("cross-tenant payment list=%v", err)
	}
	if _, err := repo.CreateProvider(ctx, orgB.org, orgB.user, orderA.ID, "cross-tenant-checkout", strings.Repeat("d", 64), false); !errors.Is(err, payment.ErrOrderNotFound) {
		t.Fatalf("cross-tenant checkout=%v", err)
	}

	unexpected := errors.New("database unavailable")
	failingPayment := payment.Payment{ID: uuid.New(), OrganizationID: uuid.New(), OrderID: uuid.New(), Status: payment.StatusPending, AmountMinor: 100, CurrencyCode: "AUD"}
	failing := &restartCheckoutRepository{payment: failingPayment, createErr: unexpected}
	service := payment.NewCheckoutService(failing, &restartProvider{}, false)
	if _, err := service.Create(ctx, organization.TenantContext{OrganizationID: failingPayment.OrganizationID, UserID: uuid.New(), Role: organization.RoleOwner}, failingPayment.OrderID, "db-error"); !errors.Is(err, unexpected) {
		t.Fatalf("unexpected repository error=%v", err)
	}
}

type restartProvider struct {
	intents map[string]payment.ProviderIntent
}

func (p *restartProvider) CreatePaymentIntent(_ context.Context, request payment.IntentRequest) (payment.ProviderIntent, error) {
	if intent, ok := p.intents[request.IdempotencyKey]; ok {
		return intent, nil
	}
	intent := payment.ProviderIntent{ID: "pi_restart", Status: "requires_payment_method", Amount: request.Amount, Currency: request.Currency, Livemode: false}
	if p.intents == nil {
		p.intents = map[string]payment.ProviderIntent{}
	}
	p.intents[request.IdempotencyKey] = intent
	return intent, nil
}
func (p *restartProvider) GetPaymentIntent(_ context.Context, id string) (payment.ProviderIntent, error) {
	for _, intent := range p.intents {
		if intent.ID == id {
			return intent, nil
		}
	}
	return payment.ProviderIntent{}, errors.New("restart provider intent not found")
}
func (p *restartProvider) CancelPaymentIntent(_ context.Context, id, _ string) (payment.ProviderIntent, error) {
	return payment.ProviderIntent{ID: id, Status: "canceled", Amount: 2000, Currency: "AUD", Livemode: false}, nil
}
func (p *restartProvider) VerifyWebhook([]byte, string) (payment.VerifiedEvent, error) {
	return payment.VerifiedEvent{}, payment.ErrInvalidWebhook
}

type restartCheckoutRepository struct {
	payment   payment.Payment
	createErr error
}

func (r *restartCheckoutRepository) CreateProvider(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, bool) (payment.Payment, error) {
	return r.payment, r.createErr
}
func (r *restartCheckoutRepository) Get(context.Context, uuid.UUID, uuid.UUID) (payment.Payment, error) {
	return r.payment, nil
}
func (r *restartCheckoutRepository) List(context.Context, uuid.UUID, uuid.UUID) ([]payment.Payment, error) {
	return []payment.Payment{r.payment}, nil
}
func (r *restartCheckoutRepository) Prepare(context.Context, uuid.UUID, uuid.UUID) (payment.Payment, error) {
	return r.payment, nil
}
func (r *restartCheckoutRepository) Bind(context.Context, payment.Payment, payment.ProviderIntent) (payment.Payment, error) {
	return r.payment, nil
}
func (r *restartCheckoutRepository) Schedule(context.Context, payment.Payment, string) (payment.Payment, error) {
	return r.payment, nil
}
func (r *restartCheckoutRepository) ConfirmCancelled(context.Context, payment.Payment, payment.ProviderIntent) (payment.Payment, error) {
	return r.payment, nil
}
func (r *restartCheckoutRepository) MarkAttention(context.Context, payment.Payment, string) (payment.Payment, error) {
	return r.payment, nil
}
func (r *restartCheckoutRepository) DuePayments(context.Context) ([]payment.Payment, error) {
	return nil, nil
}
func (r *restartCheckoutRepository) AcceptEvent(context.Context, payment.VerifiedEvent) error {
	return nil
}
func (r *restartCheckoutRepository) ProcessEvents(context.Context) error { return nil }
