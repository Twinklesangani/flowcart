package audit

import (
	"encoding/json"
	"net/http"
	"strconv"

	"flowcart/apps/api/internal/organization"
)

type Handler struct{ reader *Reader }

func NewHandler(reader *Reader) *Handler { return &Handler{reader: reader} }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	tenant, ok := organization.TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
		return
	}
	if tenant.Role != organization.RoleOwner && tenant.Role != organization.RoleAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "You do not have permission to view the audit log.")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "The audit limit is invalid.")
			return
		}
		limit = parsed
	}
	page, err := h.reader.OrganizationEvents(r.Context(), tenant.OrganizationID, Filter{Limit: limit, Cursor: r.URL.Query().Get("cursor"), EventType: r.URL.Query().Get("event_type"), ResourceType: r.URL.Query().Get("resource_type")})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "The audit query is invalid.")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
