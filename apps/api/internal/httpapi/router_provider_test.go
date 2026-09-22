package httpapi

import (
	"testing"
	"time"

	"flowcart/apps/api/internal/auth"
)

func TestRouterPropagatesStripeProviderConstructionError(t *testing.T) {
	t.Setenv("PAYMENTS_PROVIDER", "stripe")
	t.Setenv("STRIPE_SECRET_KEY", "")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "")
	_, _, err := NewRouterWithCheckout(nil, auth.NewService(nil, "test-secret", time.Minute, time.Hour), "development")
	if err == nil {
		t.Fatal("router accepted invalid stripe provider configuration")
	}
}

func TestNewRouterPropagatesStripeProviderConstructionError(t *testing.T) {
	t.Setenv("PAYMENTS_PROVIDER", "stripe")
	t.Setenv("STRIPE_SECRET_KEY", "")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "")
	if _, err := NewRouter(nil, auth.NewService(nil, "test-secret", time.Minute, time.Hour), "development"); err == nil {
		t.Fatal("NewRouter discarded invalid stripe provider configuration")
	}
}
