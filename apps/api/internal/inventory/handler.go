package inventory

import (
	"encoding/json"
	"errors"
	"flowcart/apps/api/internal/httpboundary"
	"flowcart/apps/api/internal/pagination"
	"net/http"
	"strconv"

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
	var warehouseID, productID uuid.UUID
	if raw := r.URL.Query().Get("warehouse_id"); raw != "" {
		warehouseID, err = uuid.Parse(raw)
		if err != nil {
			writeErrorFromDomain(w, ErrInvalidInput)
			return
		}
	}
	if raw := r.URL.Query().Get("product_id"); raw != "" {
		productID, err = uuid.Parse(raw)
		if err != nil {
			writeErrorFromDomain(w, ErrInvalidInput)
			return
		}
	}
	page, err := h.service.ListPage(r.Context(), tenant, limit, cursor, warehouseID, productID)
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

func (h *Handler) UpdateReplenishmentPolicy(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "inventoryID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "inventory_not_found", "Inventory not found.")
		return
	}
	var input ReplenishmentPolicyInput
	if !decode(w, r, &input) {
		return
	}
	item, err := h.service.UpdateReplenishmentPolicy(r.Context(), tenant, id, input)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) LowStock(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeErrorFromDomain(w, ErrInvalidInput)
			return
		}
		if parsed < 1 || parsed > pagination.MaxLimit {
			writeErrorFromDomain(w, ErrInvalidInput)
			return
		}
		limit = parsed
	}
	page, err := h.service.LowStockPage(r.Context(), tenant, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
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
	case errors.Is(err, ErrManagedReservation):
		writeError(w, 409, "managed_reservation", "This reservation is managed by the order payment workflow.")
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
	case errors.Is(err, ErrInvalidReplenishmentPolicy):
		writeError(w, http.StatusBadRequest, "invalid_replenishment_policy", "The replenishment policy is invalid.")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
	}
}
