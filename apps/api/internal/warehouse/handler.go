package warehouse

import (
	"encoding/json"
	"errors"
	"net/http"

	"flowcart/apps/api/internal/organization"
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
	writeJSON(w, http.StatusOK, map[string]any{"warehouses": items})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "warehouseID"))
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
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	tenant, ok := tenantFromRequest(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "warehouseID"))
	if err != nil {
		writeErrorFromDomain(w, ErrNotFound)
		return
	}
	var patch PatchInput
	if !decode(w, r, &patch) {
		return
	}
	item, err := h.service.Update(r.Context(), tenant, id, patch)
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
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "warehouse_not_found", "Warehouse not found.")
	case errors.Is(err, ErrCodeTaken):
		writeError(w, http.StatusConflict, "warehouse_code_taken", "That warehouse code is already in use.")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
	}
}
