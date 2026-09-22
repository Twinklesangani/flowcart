package fulfillment

import (
	"encoding/json"
	"errors"
	"flowcart/apps/api/internal/httpboundary"
	"net/http"
	"strconv"

	"flowcart/apps/api/internal/organization"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	orderID, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		writeDomainError(w, ErrOrderNotFound)
		return
	}

	var input CreateInput
	if !httpboundary.Decode(w, r, &input, httpboundary.MutationBodyLimit) {
		return
	}
	item, err := h.service.Complete(r.Context(), tenant, orderID, r.Header.Get("Idempotency-Key"), input)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	orderID, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		writeDomainError(w, ErrOrderNotFound)
		return
	}
	items, err := h.service.List(r.Context(), tenant, orderID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fulfillments": items})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "fulfillmentID"))
	if err != nil {
		writeDomainError(w, ErrNotFound)
		return
	}
	item, err := h.service.Get(r.Context(), tenant, id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) Movements(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "inventoryID"))
	if err != nil {
		writeDomainError(w, ErrNotFound)
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			writeDomainError(w, ErrInvalidInput)
			return
		}
	}
	items, err := h.service.Movements(r.Context(), tenant, id, limit)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"movements": items})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeError(w, 400, "invalid_request", "Request data is invalid.")
	case errors.Is(err, ErrForbidden):
		writeError(w, 403, "forbidden", "You do not have permission for this action.")
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrOrderNotFound), errors.Is(err, ErrReservationNotFound):
		writeError(w, 404, "not_found", "The requested fulfillment resource was not found.")
	case errors.Is(err, ErrOrderNotPaid):
		writeError(w, 409, "order_not_paid", "The order is not eligible for fulfillment.")
	case errors.Is(err, ErrOrderAlreadyFulfilled):
		writeError(w, 409, "order_already_fulfilled", "The order is already fulfilled.")
	case errors.Is(err, ErrReservationNotCommitted):
		writeError(w, 409, "reservation_not_committed", "The reservation is not committed.")
	case errors.Is(err, ErrReservationAlreadyConsumed):
		writeError(w, 409, "reservation_already_consumed", "The reservation was already consumed.")
	case errors.Is(err, ErrReservationOtherOrder):
		writeError(w, 409, "reservation_belongs_to_other_order", "The reservation belongs to another order.")
	case errors.Is(err, ErrMixedWarehouse):
		writeError(w, 409, "mixed_warehouse_fulfillment", "A fulfillment can use one warehouse only.")
	case errors.Is(err, ErrInsufficientStock):
		writeError(w, 409, "insufficient_stock", "There is not enough physical stock to fulfill the reservation.")
	case errors.Is(err, ErrIdempotencyKeyReused):
		writeError(w, 409, "idempotency_key_reused", "That idempotency key was used for a different request.")
	default:
		writeError(w, 500, "internal_error", "An unexpected error occurred.")
	}
}
