package payment

import (
	"encoding/json"
	"errors"
	"net/http"

	"flowcart/apps/api/internal/organization"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	service  *Service
	checkout *CheckoutService
}

func NewHandler(service *Service, checkout ...*CheckoutService) *Handler {
	handler := &Handler{service: service}
	if len(checkout) > 0 {
		handler.checkout = checkout[0]
	}
	return handler
}

func (h *Handler) Checkout(w http.ResponseWriter, r *http.Request) {
	if h.checkout == nil {
		writeError(w, 503, "provider_unavailable", "Payment provider is unavailable.")
		return
	}
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", "Authentication is required.")
		return
	}
	orderID, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		writeErrorFromDomain(w, ErrOrderNotFound)
		return
	}
	result, err := h.checkout.Create(r.Context(), tenant, orderID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	h.writeCheckoutResult(w, result)
}

func (h *Handler) writeCheckoutResult(w http.ResponseWriter, result CheckoutResult) {
	if result.ClientSecret != "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	if h.checkout == nil {
		writeError(w, 503, "provider_unavailable", "Payment provider is unavailable.")
		return
	}
	h.checkout.Webhook(w, r)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", "Authentication is required.")
		return
	}
	orderID, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		writeErrorFromDomain(w, ErrOrderNotFound)
		return
	}
	payment, err := h.service.Create(r.Context(), tenant, orderID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, payment)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", "Authentication is required.")
		return
	}
	orderID, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		writeErrorFromDomain(w, ErrOrderNotFound)
		return
	}
	payments, err := h.service.List(r.Context(), tenant, orderID)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"payments": payments})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", "Authentication is required.")
		return
	}
	paymentID, err := uuid.Parse(chi.URLParam(r, "paymentID"))
	if err != nil {
		writeErrorFromDomain(w, ErrPaymentNotFound)
		return
	}
	payment, err := h.service.Get(r.Context(), tenant, paymentID)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, 200, payment)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func writeErrorFromDomain(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeError(w, 400, "invalid_request", "Request data is invalid.")
	case errors.Is(err, ErrForbidden):
		writeError(w, 403, "forbidden", "You do not have permission for this action.")
	case errors.Is(err, ErrOrderNotFound):
		writeError(w, 404, "order_not_found", "Order not found.")
	case errors.Is(err, ErrPaymentNotFound):
		writeError(w, 404, "payment_not_found", "Payment not found.")
	case errors.Is(err, ErrOrderCancelled):
		writeError(w, 409, "order_cancelled", "The order is cancelled.")
	case errors.Is(err, ErrReservationExpired):
		writeError(w, 409, "reservation_expired", "The order no longer has reserved stock.")
	case errors.Is(err, ErrPaymentInProgress):
		writeError(w, 409, "payment_in_progress", "A payment attempt is already pending.")
	case errors.Is(err, ErrPaymentAlreadySucceeded):
		writeError(w, 409, "payment_already_succeeded", "The order has already been paid.")
	case errors.Is(err, ErrPaymentNotRequired):
		writeError(w, 409, "payment_not_required", "No payment is required for this order.")
	case errors.Is(err, ErrIdempotencyKeyReused):
		writeError(w, 409, "idempotency_key_reused", "That idempotency key was used for a different request.")
	default:
		writeError(w, 500, "internal_error", "An unexpected error occurred.")
	}
}
