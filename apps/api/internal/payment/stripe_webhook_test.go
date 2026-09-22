package payment

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
)

func signedStripePayload(t *testing.T, eventType string, created time.Time) ([]byte, string) {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"id":"evt_test_123","object":"event","api_version":"%s","created":%d,"data":{"object":{"id":"pi_test_123","object":"payment_intent","amount":100,"amount_received":100,"currency":"aud","livemode":false,"status":"succeeded"}},"livemode":false,"pending_webhooks":1,"type":"%s"}`, stripe.APIVersion, created.Unix(), eventType))
	secret := "whsec_test_only"
	signature := fmt.Sprintf("t=%d,v1=%x", created.Unix(), stripe.ComputeSignature(created, body, secret))
	return body, signature
}

func TestStripeProviderWebhookSignatureValidation(t *testing.T) {
	provider, err := NewStripeProvider("sk_test_only", "whsec_test_only")
	if err != nil {
		t.Fatal(err)
	}
	body, signature := signedStripePayload(t, "payment_intent.succeeded", time.Now())
	if event, err := provider.VerifyWebhook(body, signature); err != nil || event.ID != "evt_test_123" || event.IntentID != "pi_test_123" {
		t.Fatalf("valid webhook = %+v, %v", event, err)
	}
	altered := append([]byte(nil), body...)
	altered[len(altered)-2] = 'x'
	if _, err := provider.VerifyWebhook(altered, signature); err == nil {
		t.Fatal("altered body accepted")
	}
	staleBody, staleSignature := signedStripePayload(t, "payment_intent.succeeded", time.Now().Add(-10*time.Minute))
	if _, err := provider.VerifyWebhook(staleBody, staleSignature); err == nil {
		t.Fatal("stale signature accepted")
	}
	if _, err := provider.VerifyWebhook([]byte("not-json"), signature); err == nil {
		t.Fatal("malformed payload accepted")
	}
}

func TestStripeWebhookHTTPSizeAndSignatureBehavior(t *testing.T) {
	provider, err := NewStripeProvider("sk_test_only", "whsec_test_only")
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeCheckoutRepository{payment: fakePayment()}
	service := NewCheckoutService(repo, provider, false)
	handler := NewHandler(NewService(nil), service)
	body, signature := signedStripePayload(t, "payment_intent.created", time.Now())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/stripe", strings.NewReader(string(body)))
	request.Header.Set("Stripe-Signature", signature)
	response := httptest.NewRecorder()
	handler.Webhook(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unsupported webhook status=%d", response.Code)
	}
	if len(repo.accepted) != 1 {
		t.Fatalf("accepted events=%d", len(repo.accepted))
	}

	missing := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/stripe", strings.NewReader(string(body)))
	missingResponse := httptest.NewRecorder()
	handler.Webhook(missingResponse, missing)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing signature status=%d", missingResponse.Code)
	}

	oversized := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/stripe", strings.NewReader(strings.Repeat("x", int(MaxWebhookBytes)+1)))
	oversized.Header.Set("Stripe-Signature", signature)
	oversizedResponse := httptest.NewRecorder()
	handler.Webhook(oversizedResponse, oversized)
	if oversizedResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status=%d", oversizedResponse.Code)
	}
}
