package product

import (
	"encoding/json"
	"errors"
	"flowcart/apps/api/internal/httpboundary"
	"flowcart/apps/api/internal/pagination"
	"net/http"

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
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
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
	page, err := h.service.ListPage(r.Context(), tenant, limit, cursor)
	if err != nil {
		writeErrorFromDomain(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "productID"))
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
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "productID"))
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
		writeError(w, http.StatusBadRequest, "invalid_request", "Request data is invalid.")
	case errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "You do not have permission for this action.")
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "product_not_found", "Product not found.")
	case errors.Is(err, ErrSKUTaken):
		writeError(w, http.StatusConflict, "sku_taken", "That SKU is already in use.")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
	}
}
