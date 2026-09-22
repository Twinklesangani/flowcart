package payment

import (
	"errors"
	"io"
	"net/http"
)

const MaxWebhookBytes int64 = 1 << 20

func (s *CheckoutService) Webhook(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		writeError(w, 503, "provider_unavailable", "Payment provider is unavailable.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxWebhookBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			w.WriteHeader(413)
		} else {
			w.WriteHeader(400)
		}
		return
	}
	if r.Header.Get("Stripe-Signature") == "" {
		w.WriteHeader(400)
		return
	}
	if err = s.Receive(r.Context(), body, r.Header.Get("Stripe-Signature")); err != nil {
		if errors.Is(err, ErrInvalidWebhook) {
			w.WriteHeader(400)
		} else {
			w.WriteHeader(503)
		}
		return
	}
	w.WriteHeader(200)
}
