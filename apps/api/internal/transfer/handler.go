package transfer

import (
	"context"
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
		writeError(w, 401, "unauthorized", "Authentication is required.")
		return
	}

	var input CreateInput
	if !httpboundary.Decode(w, r, &input, httpboundary.MutationBodyLimit) {
		return
	}
	item, err := h.service.Create(r.Context(), tenant, r.Header.Get("Idempotency-Key"), input)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", "Authentication is required.")
		return
	}
	limit, err := pagination.ParseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeDomainError(w, ErrInvalidInput)
		return
	}
	var cursor pagination.Cursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, err = pagination.DecodeCursor(raw, tenant.OrganizationID)
		if err != nil {
			writeDomainError(w, ErrInvalidInput)
			return
		}
	}
	page, err := h.service.ListPage(r.Context(), tenant, limit, cursor, r.URL.Query().Get("status"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, page)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", "Authentication is required.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "transferID"))
	if err != nil {
		writeDomainError(w, ErrNotFound)
		return
	}
	item, err := h.service.Get(r.Context(), tenant, id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, item)
}

func (h *Handler) Timeline(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", "Authentication is required.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "transferID"))
	if err != nil {
		writeDomainError(w, ErrNotFound)
		return
	}
	events, err := h.service.Timeline(r.Context(), tenant, id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"events": events})
}

func (h *Handler) Dispatch(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, h.service.Dispatch, http.StatusOK)
}
func (h *Handler) Receive(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, h.service.Receive, http.StatusOK)
}
func (h *Handler) mutate(w http.ResponseWriter, r *http.Request, operation func(context.Context, organization.TenantContext, uuid.UUID, string) (Transfer, error), status int) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", "Authentication is required.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "transferID"))
	if err != nil {
		writeDomainError(w, ErrNotFound)
		return
	}
	item, err := operation(r.Context(), tenant, id, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, status, item)
}

func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", "Authentication is required.")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "transferID"))
	if err != nil {
		writeDomainError(w, ErrNotFound)
		return
	}
	item, err := h.service.Cancel(r.Context(), tenant, id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, 200, item)
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
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrSourceInventoryNotFound):
		writeError(w, 404, "not_found", "The requested transfer resource was not found.")
	case errors.Is(err, ErrIdempotencyKeyReused):
		writeError(w, 409, "idempotency_key_reused", "That idempotency key was used for a different request.")
	case errors.Is(err, ErrSameWarehouse):
		writeError(w, 400, "same_warehouse_transfer", "Source and destination warehouses must differ.")
	case errors.Is(err, ErrTransferNotPending):
		writeError(w, 409, "transfer_not_pending", "The transfer is not pending.")
	case errors.Is(err, ErrTransferNotInTransit):
		writeError(w, 409, "transfer_not_in_transit", "The transfer is not in transit.")
	case errors.Is(err, ErrTransferAlreadyCompleted):
		writeError(w, 409, "transfer_already_completed", "The transfer is already completed.")
	case errors.Is(err, ErrInsufficientTransferableStock):
		writeError(w, 409, "insufficient_transferable_stock", "There is not enough transferable stock.")
	case errors.Is(err, ErrInactiveWarehouse):
		writeError(w, 409, "inactive_warehouse", "A transfer warehouse is inactive.")
	case errors.Is(err, ErrInactiveProduct):
		writeError(w, 409, "inactive_product", "A transfer product is inactive.")
	default:
		writeError(w, 500, "internal_error", "An unexpected error occurred.")
	}
}
