package inventory

import (
	"encoding/json"
	"errors"
	"net/http"

	"flowcart/apps/api/internal/organization"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct{ service *Service }
type adjustInput struct {
	Delta int64 `json:"delta"`
}
type reserveInput struct {
	Quantity int64 `json:"quantity"`
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	var input CreateInput
	if !decode(w, r, &input) {
		return
	}
	item, err := h.service.Create(r.Context(), tenant, input)
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
	items, err := h.service.List(r.Context(), tenant)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"inventory": items})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "inventoryID"))
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
func (h *Handler) Adjust(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "inventoryID"))
	if err != nil {
		writeErrorFromDomain(w, ErrNotFound)
		return
	}
	var input adjustInput
	if !decode(w, r, &input) {
		return
	}
	item, err := h.service.Adjust(r.Context(), tenant, id, input.Delta)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (h *Handler) Reserve(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	inventoryID, err := uuid.Parse(chi.URLParam(r, "inventoryID"))
	if err != nil {
		writeErrorFromDomain(w, ErrNotFound)
		return
	}
	var input reserveInput
	if !decode(w, r, &input) {
		return
	}
	reservation, err := h.service.Reserve(r.Context(), tenant, inventoryID, input.Quantity)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, reservation)
}
func (h *Handler) Release(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	reservationID, err := uuid.Parse(chi.URLParam(r, "reservationID"))
	if err != nil {
		writeErrorFromDomain(w, ErrReservationNotFound)
		return
	}
	reservation, err := h.service.Release(r.Context(), tenant, reservationID)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reservation)
}
func tenantFromRequest(w http.ResponseWriter, r *http.Request) (organization.TenantContext, bool) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
	}
	return tenant, ok
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if json.NewDecoder(r.Body).Decode(target) != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Request data is invalid.")
		return false
	}
	return true
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
		writeError(w, http.StatusBadRequest, "invalid_request", "Request data is invalid.")
	case errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "You do not have permission for this action.")
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrProductNotFound), errors.Is(err, ErrWarehouseNotFound):
		writeError(w, http.StatusNotFound, "inventory_not_found", "Inventory or related resource not found.")
	case errors.Is(err, ErrInventoryExists):
		writeError(w, http.StatusConflict, "inventory_exists", "That product and warehouse already have an inventory level.")
	case errors.Is(err, ErrInactiveProduct):
		writeError(w, http.StatusConflict, "inactive_product", "The product is inactive.")
	case errors.Is(err, ErrInactiveWarehouse):
		writeError(w, http.StatusConflict, "inactive_warehouse", "The warehouse is inactive.")
	case errors.Is(err, ErrInsufficientStock):
		writeError(w, http.StatusConflict, "insufficient_stock", "The adjustment would make stock negative.")
	case errors.Is(err, ErrStockBelowReserved):
		writeError(w, http.StatusConflict, "stock_below_reserved", "Stock cannot fall below the reserved quantity.")
	case errors.Is(err, ErrInsufficientAvailableStock):
		writeError(w, http.StatusConflict, "insufficient_available_stock", "There is not enough available stock for this reservation.")
	case errors.Is(err, ErrReservationNotFound):
		writeError(w, http.StatusNotFound, "reservation_not_found", "Reservation not found.")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
	}
}
