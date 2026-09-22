package order

import (
	"encoding/json"
	"errors"
	"flowcart/apps/api/internal/httpboundary"
	"net/http"

	"flowcart/apps/api/internal/inventory"
	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/pagination"
	"flowcart/apps/api/internal/payment"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	var input CreateInput
	if !decode(w, r, &input) {
		return
	}
	item, err := h.service.Create(r.Context(), tenant, key, input)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
func (h *Handler) AutoCreate(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	var input AutoCreateInput
	if !decode(w, r, &input) {
		return
	}
	item, err := h.service.AutoCreate(r.Context(), tenant, r.Header.Get("Idempotency-Key"), input)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	limit, err := pagination.ParseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeErrorFromDomain(w, ErrInvalidInput)
		return
	}
	var cursor pagination.Cursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, err = pagination.DecodeCursor(raw, tenant.OrganizationID)
		if err != nil {
			writeErrorFromDomain(w, ErrInvalidInput)
			return
		}
	}
	page, err := h.service.List(r.Context(), tenant, limit, cursor, r.URL.Query().Get("status"), r.URL.Query().Get("search"))
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		writeErrorFromDomain(w, ErrNotFound)
		return
	}
	item, err := h.service.Get(r.Context(), tenant, id)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (h *Handler) Timeline(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "order_not_found", "Order not found.")
		return
	}
	events, err := h.service.Timeline(r.Context(), tenant, id)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "orderID"))
	if err != nil {
		writeErrorFromDomain(w, ErrNotFound)
		return
	}
	item, err := h.service.Cancel(r.Context(), tenant, id)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func tenantFromRequest(w http.ResponseWriter, r *http.Request) (organization.TenantContext, bool) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
	}
	return tenant, ok
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	return httpboundary.Decode(w, r, target, httpboundary.MutationBodyLimit)
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
	case errors.Is(err, ErrNotFound):
		writeError(w, 404, "order_not_found", "Order not found.")
	case errors.Is(err, inventory.ErrNotFound):
		writeError(w, 404, "inventory_not_found", "Inventory not found.")
	case errors.Is(err, inventory.ErrInactiveProduct):
		writeError(w, 409, "inactive_product", "The product is inactive.")
	case errors.Is(err, inventory.ErrInactiveWarehouse):
		writeError(w, 409, "inactive_warehouse", "The warehouse is inactive.")
	case errors.Is(err, ErrInsufficientAvailableStock):
		writeError(w, 409, "insufficient_available_stock", "There is not enough available stock.")
	case errors.Is(err, ErrInsufficientNetworkStock):
		writeError(w, 409, "insufficient_network_stock", "There is not enough stock across the organization.")
	case errors.Is(err, ErrNoEligibleWarehouse):
		writeError(w, 409, "no_eligible_warehouse", "No eligible warehouse can fulfill the request.")
	case errors.Is(err, ErrProductPriceMissing):
		writeError(w, 409, "product_price_missing", "The product does not have a complete price.")
	case errors.Is(err, ErrAutomaticAllocationComplexity):
		writeError(w, 422, "automatic_allocation_too_complex", "The automatic allocation request exceeds the safe complexity budget.")
	case errors.Is(err, ErrMixedCurrencyOrder):
		writeError(w, 409, "mixed_currency_order", "All order items must use the same currency.")
	case errors.Is(err, ErrOrderTotalOverflow):
		writeError(w, 400, "order_total_overflow", "The order total is too large.")
	case errors.Is(err, ErrIdempotencyKeyReused):
		writeError(w, 409, "idempotency_key_reused", "That idempotency key was used for a different request.")
	case errors.Is(err, ErrOrderNotCancellable):
		writeError(w, 409, "order_not_cancellable", "The order cannot be cancelled.")
	case errors.Is(err, payment.ErrPaymentAlreadySucceeded):
		writeError(w, 409, "payment_already_succeeded", "The order has already been paid.")
	default:
		writeError(w, 500, "internal_error", "An unexpected error occurred.")
	}
}
