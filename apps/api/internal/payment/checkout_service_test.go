package payment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"flowcart/apps/api/internal/organization"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type fakeProvider struct {
	createCalls []IntentRequest
	getCalls    []string
	cancelCalls []string
	intents     map[string]ProviderIntent
	createErr   error
	getErr      error
	cancelErr   error
	webhook     VerifiedEvent
	webhookErr  error
}

func (p *fakeProvider) CreatePaymentIntent(_ context.Context, request IntentRequest) (ProviderIntent, error) {
	p.createCalls = append(p.createCalls, request)
	if p.createErr != nil {
		return ProviderIntent{}, p.createErr
	}
	if intent, ok := p.intents[request.IdempotencyKey]; ok {
		return intent, nil
	}
	intent := ProviderIntent{ID: "pi_fake_123", Status: "requires_payment_method", Amount: request.Amount, Currency: request.Currency, ClientSecret: "cs_test_only", Livemode: false}
	if p.intents == nil {
		p.intents = map[string]ProviderIntent{}
	}
	p.intents[request.IdempotencyKey] = intent
	return intent, nil
}

func (p *fakeProvider) GetPaymentIntent(_ context.Context, id string) (ProviderIntent, error) {
	p.getCalls = append(p.getCalls, id)
	if p.getErr != nil {
		return ProviderIntent{}, p.getErr
	}
	for _, intent := range p.intents {
		if intent.ID == id {
			return intent, nil
		}
	}
	return ProviderIntent{}, errors.New("fake intent not found")
}

func (p *fakeProvider) CancelPaymentIntent(_ context.Context, id, key string) (ProviderIntent, error) {
	p.cancelCalls = append(p.cancelCalls, id+"|"+key)
	if p.cancelErr != nil {
		return ProviderIntent{}, p.cancelErr
	}
	return ProviderIntent{ID: id, Status: "canceled", Amount: 100, Currency: "AUD", Livemode: false}, nil
}

func (p *fakeProvider) VerifyWebhook([]byte, string) (VerifiedEvent, error) {
	return p.webhook, p.webhookErr
}

type fakeCheckoutRepository struct {
	payment       Payment
	createErr     error
	bindErr       error
	bindCalls     int
	scheduleCalls int
	processCalls  int
	dueCalls      int
	processCalled chan struct{}
	attention     string
	accepted      []VerifiedEvent
}

func (r *fakeCheckoutRepository) CreateProvider(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, bool) (Payment, error) {
	if r.createErr != nil {
		return Payment{}, r.createErr
	}
	return r.payment, nil
}
func (r *fakeCheckoutRepository) Get(context.Context, uuid.UUID, uuid.UUID) (Payment, error) {
	return r.payment, nil
}
func (r *fakeCheckoutRepository) List(context.Context, uuid.UUID, uuid.UUID) ([]Payment, error) {
	return []Payment{r.payment}, nil
}
func (r *fakeCheckoutRepository) Prepare(context.Context, uuid.UUID, uuid.UUID) (Payment, error) {
	return r.payment, nil
}
func (r *fakeCheckoutRepository) Bind(_ context.Context, p Payment, intent ProviderIntent) (Payment, error) {
	r.bindCalls++
	if r.bindErr != nil {
		return Payment{}, r.bindErr
	}
	providerID := intent.ID
	providerStatus := intent.Status
	p.ProviderPaymentID = &providerID
	p.ProviderStatus = &providerStatus
	r.payment = p
	return p, nil
}
func (r *fakeCheckoutRepository) Schedule(context.Context, Payment, string) (Payment, error) {
	r.scheduleCalls++
	return r.payment, nil
}
func (r *fakeCheckoutRepository) ConfirmCancelled(context.Context, Payment, ProviderIntent) (Payment, error) {
	return r.payment, nil
}
func (r *fakeCheckoutRepository) MarkAttention(_ context.Context, p Payment, reason string) (Payment, error) {
	r.attention = reason
	return p, nil
}
func (r *fakeCheckoutRepository) DuePayments(context.Context) ([]Payment, error) {
	r.dueCalls++
	return nil, nil
}
func (r *fakeCheckoutRepository) AcceptEvent(_ context.Context, event VerifiedEvent) error {
	r.accepted = append(r.accepted, event)
	return nil
}
func (r *fakeCheckoutRepository) ProcessEvents(context.Context) error {
	r.processCalls++
	if r.processCalled != nil {
		select {
		case r.processCalled <- struct{}{}:
		default:
		}
	}
	return nil
}

func fakePayment() Payment {
	live := false
	provider := "stripe"
	deadline := time.Now().Add(time.Hour)
	return Payment{ID: uuid.New(), OrganizationID: uuid.New(), OrderID: uuid.New(), Status: StatusPending, AmountMinor: 100, CurrencyCode: "AUD", ProviderName: &provider, ProviderLivemode: &live, CheckoutDeadlineAt: &deadline}
}

func fakeTenant() organization.TenantContext {
	return organization.TenantContext{OrganizationID: uuid.New(), UserID: uuid.New(), Role: organization.RoleOwner}
}

func TestCheckoutFakeProviderCreationAndTransientClientSecret(t *testing.T) {
	repo := &fakeCheckoutRepository{payment: fakePayment()}
	provider := &fakeProvider{}
	service := NewCheckoutService(repo, provider, false)
	result, err := service.Create(context.Background(), fakeTenant(), repo.payment.OrderID, "checkout-key")
	if err != nil {
		t.Fatal(err)
	}
	if result.ClientSecret != "cs_test_only" {
		t.Fatalf("client secret = %q", result.ClientSecret)
	}
	if len(provider.createCalls) != 1 {
		t.Fatalf("create calls = %d", len(provider.createCalls))
	}
	request := provider.createCalls[0]
	if request.Amount != 100 || request.Currency != "AUD" || request.IdempotencyKey != providerKey(repo.payment.ID) {
		t.Fatalf("request = %+v", request)
	}
	if strings.Contains(string(mustJSON(t, result.Payment)), "client_secret") {
		t.Fatal("payment contains client secret")
	}
}

func TestCheckoutHandlerClientSecretIsNoStoreAndPaymentViewsRedactIt(t *testing.T) {
	handler := NewHandler(NewService(&fakeRepository{}))
	result := CheckoutResult{Payment: fakePayment(), ClientSecret: "client_secret_fake_test_only"}
	response := httptest.NewRecorder()
	handler.writeCheckoutResult(response, result)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control = %q", response.Header().Get("Cache-Control"))
	}
	if !strings.Contains(response.Body.String(), "client_secret_fake_test_only") {
		t.Fatal("initialization response omitted transient client secret")
	}
	paymentJSON := mustJSON(t, result.Payment)
	if strings.Contains(string(paymentJSON), "client_secret") {
		t.Fatal("payment GET/LIST representation contains client secret")
	}
}

func TestCheckoutFakeProviderFailureAndDisabledBehavior(t *testing.T) {
	repo := &fakeCheckoutRepository{payment: fakePayment()}
	provider := &fakeProvider{createErr: ErrProviderRequest}
	result, err := NewCheckoutService(repo, provider, false).Create(context.Background(), fakeTenant(), repo.payment.OrderID, "failure-key")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Processing || repo.scheduleCalls != 1 {
		t.Fatalf("result=%+v schedule=%d", result, repo.scheduleCalls)
	}
	if _, err := NewCheckoutService(repo, nil, false).Create(context.Background(), fakeTenant(), repo.payment.OrderID, "disabled-key"); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("disabled error = %v", err)
	}
}

func TestCheckoutFakeProviderRetryUsesStableIdempotencyAndRetrieves(t *testing.T) {
	repo := &fakeCheckoutRepository{payment: fakePayment(), bindErr: errors.New("binding failed")}
	provider := &fakeProvider{}
	service := NewCheckoutService(repo, provider, false)
	if _, err := service.Create(context.Background(), fakeTenant(), repo.payment.OrderID, "retry-key"); err == nil {
		t.Fatal("first binding should fail")
	}
	repo.bindErr = nil
	if _, err := service.Create(context.Background(), fakeTenant(), repo.payment.OrderID, "retry-key"); err != nil {
		t.Fatal(err)
	}
	if len(provider.createCalls) != 2 || provider.createCalls[0].IdempotencyKey != provider.createCalls[1].IdempotencyKey {
		t.Fatalf("create calls = %+v", provider.createCalls)
	}
	if provider.createCalls[0].PaymentID != repo.payment.ID {
		t.Fatal("payment identity changed")
	}
	providerIntent := provider.intents[provider.createCalls[0].IdempotencyKey]
	if repo.payment.ProviderPaymentID == nil || *repo.payment.ProviderPaymentID != providerIntent.ID {
		t.Fatal("provider binding missing")
	}

	repo.payment.ProviderPaymentID = &providerIntent.ID
	if _, err := service.Create(context.Background(), fakeTenant(), repo.payment.OrderID, "retrieve-key"); err != nil {
		t.Fatal(err)
	}
	if len(provider.getCalls) != 1 || provider.getCalls[0] != providerIntent.ID {
		t.Fatalf("get calls = %v", provider.getCalls)
	}
}

func TestCheckoutDeadlineIsNotExtendedAndProviderlessPaymentIsNotUpgraded(t *testing.T) {
	payment := fakePayment()
	deadline := time.Now().Add(10 * time.Minute)
	payment.CheckoutDeadlineAt = &deadline
	repo := &fakeCheckoutRepository{payment: payment}
	provider := &fakeProvider{}
	service := NewCheckoutService(repo, provider, false)
	if _, err := service.Create(context.Background(), fakeTenant(), payment.OrderID, "deadline-key"); err != nil {
		t.Fatal(err)
	}
	if !repo.payment.CheckoutDeadlineAt.Equal(deadline) {
		t.Fatal("checkout deadline changed")
	}

	repo.payment = Payment{ID: uuid.New(), OrganizationID: payment.OrganizationID, OrderID: payment.OrderID, Status: StatusPending, AmountMinor: 100, CurrencyCode: "AUD"}
	if _, err := service.Create(context.Background(), fakeTenant(), repo.payment.OrderID, "legacy-key"); err != nil {
		t.Fatal(err)
	}
	if len(provider.createCalls) != 1 {
		t.Fatal("providerless payment was sent to provider")
	}
}

func TestCheckoutCancellationUncertaintyAndWebhookHTTP(t *testing.T) {
	payment := fakePayment()
	now := time.Now()
	payment.CancelRequestedAt = &now
	repo := &fakeCheckoutRepository{payment: payment}
	provider := &fakeProvider{intents: map[string]ProviderIntent{"existing": {ID: "pi_existing", Status: "requires_action", Amount: 100, Currency: "AUD", Livemode: false}}, cancelErr: ErrProviderRequest}
	payment.ProviderPaymentID = stringPtr("pi_existing")
	service := NewCheckoutService(repo, provider, false)
	if _, err := service.Create(context.Background(), fakeTenant(), payment.OrderID, "cancel-key"); err != nil {
		t.Fatal(err)
	}
	if len(provider.cancelCalls) != 1 || repo.scheduleCalls != 1 {
		t.Fatalf("cancel=%v schedule=%d", provider.cancelCalls, repo.scheduleCalls)
	}

	provider.webhook = VerifiedEvent{ID: "evt_1", Type: "payment_intent.succeeded", Provider: "stripe", IntentID: "pi_existing", PayloadHash: strings.Repeat("a", 64), Livemode: false}
	handler := NewHandler(NewService(nil), service)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("orderID", payment.OrderID.String())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/stripe", strings.NewReader(`{"id":"evt_1"}`)).WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, routeContext))
	req.Header.Set("Stripe-Signature", "test")
	res := httptest.NewRecorder()
	handler.Webhook(res, req)
	if res.Code != http.StatusOK || len(repo.accepted) != 1 {
		t.Fatalf("webhook status=%d accepted=%d", res.Code, len(repo.accepted))
	}
	missing := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/stripe", strings.NewReader(`{}`))
	missingRes := httptest.NewRecorder()
	handler.Webhook(missingRes, missing)
	if missingRes.Code != http.StatusBadRequest {
		t.Fatalf("missing signature status=%d", missingRes.Code)
	}
}

func TestCheckoutRunStartsOnceAndStopsOnCancellation(t *testing.T) {
	repo := &fakeCheckoutRepository{processCalled: make(chan struct{}, 2)}
	service := NewCheckoutService(repo, &fakeProvider{}, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.Run(ctx, nil)
		close(done)
	}()
	select {
	case <-repo.processCalled:
	case <-time.After(time.Second):
		t.Fatal("reconciliation did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconciliation did not stop after cancellation")
	}
	if repo.dueCalls != 1 {
		t.Fatalf("reconciliation passes = %d, want 1", repo.dueCalls)
	}
}

func TestCheckoutRunDoesNothingWhenProviderIsDisabled(t *testing.T) {
	repo := &fakeCheckoutRepository{processCalled: make(chan struct{}, 1)}
	NewCheckoutService(repo, nil, false).Run(context.Background(), nil)
	select {
	case <-repo.processCalled:
		t.Fatal("disabled provider started reconciliation")
	default:
	}
}

func stringPtr(value string) *string { return &value }
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
