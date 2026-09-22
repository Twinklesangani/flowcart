package payment

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
)

type blockingStripeTransport struct {
	started  chan struct{}
	canceled chan struct{}
}

func (transport *blockingStripeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	close(transport.started)
	<-request.Context().Done()
	close(transport.canceled)
	return nil, request.Context().Err()
}

func TestStripeProviderPropagatesCancellationContext(t *testing.T) {
	transport := &blockingStripeTransport{started: make(chan struct{}), canceled: make(chan struct{})}
	oldBackend := stripe.GetBackend(stripe.APIBackend)
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{HTTPClient: &http.Client{Transport: transport}}))
	t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, oldBackend) })

	provider, err := NewStripeProvider("sk_test_only", "whsec_test_only")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, requestErr := provider.GetPaymentIntent(ctx, "pi_test")
		result <- requestErr
	}()
	<-transport.started
	cancel()

	select {
	case <-transport.canceled:
	case <-time.After(time.Second):
		t.Fatal("Stripe transport did not observe cancellation")
	}
	if err := <-result; !errors.Is(err, ErrProviderRequest) {
		t.Fatalf("provider error = %v, want %v", err, ErrProviderRequest)
	}
}
