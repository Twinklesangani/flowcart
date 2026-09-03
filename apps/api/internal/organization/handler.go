package organization

import (
	"encoding/json"
	"errors"
	"flowcart/apps/api/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

type organizationInput struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}
type memberInput struct {
	Email string `json:"email"`
	Role  Role   `json:"role"`
}
type organizationResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
	Role Role      `json:"role,omitempty"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeUnauthorized(w)
		return
	}
	var in organizationInput
	if !decode(w, r, &in) {
		return
	}
	o, err := h.service.Create(r.Context(), userID, in.Name, in.Slug)
	if err != nil {
		writeOrganizationError(w, err)
		return
	}
	writeOrganizationJSON(w, http.StatusCreated, organizationResponse{ID: o.ID, Name: o.Name, Slug: o.Slug})
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	items, err := h.service.List(r.Context(), userID)
	if err != nil {
		writeOrganizationError(w, err)
		return
	}
	result := make([]organizationResponse, 0, len(items))
	for _, o := range items {
		result = append(result, organizationResponse{ID: o.ID, Name: o.Name, Slug: o.Slug, Role: o.Role})
	}
	writeOrganizationJSON(w, http.StatusOK, map[string]any{"organizations": result})
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "organizationID"))
	if err != nil {
		writeOrganizationError(w, ErrNotFound)
		return
	}
	o, err := h.service.Get(r.Context(), userID, id)
	if err != nil {
		writeOrganizationError(w, err)
		return
	}
	writeOrganizationJSON(w, http.StatusOK, organizationResponse{ID: o.ID, Name: o.Name, Slug: o.Slug})
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "organizationID"))
	if err != nil {
		writeOrganizationError(w, ErrNotFound)
		return
	}
	var in organizationInput
	if !decode(w, r, &in) {
		return
	}
	o, err := h.service.Update(r.Context(), userID, id, in.Name, in.Slug)
	if err != nil {
		writeOrganizationError(w, err)
		return
	}
	writeOrganizationJSON(w, http.StatusOK, organizationResponse{ID: o.ID, Name: o.Name, Slug: o.Slug})
}
func (h *Handler) Members(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "organizationID"))
	if err != nil {
		writeOrganizationError(w, ErrNotFound)
		return
	}
	members, err := h.service.Members(r.Context(), userID, id)
	if err != nil {
		writeOrganizationError(w, err)
		return
	}
	writeOrganizationJSON(w, http.StatusOK, map[string]any{"members": members})
}
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "organizationID"))
	if err != nil {
		writeOrganizationError(w, ErrNotFound)
		return
	}
	var in memberInput
	if !decode(w, r, &in) {
		return
	}
	if err = h.service.AddMember(r.Context(), userID, id, in.Email, in.Role); err != nil {
		writeOrganizationError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
func (h *Handler) ChangeRole(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "organizationID"))
	target, parseErr := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil || parseErr != nil {
		writeOrganizationError(w, ErrMemberNotFound)
		return
	}
	var in memberInput
	if !decode(w, r, &in) {
		return
	}
	if err = h.service.ChangeRole(r.Context(), userID, id, target, in.Role); err != nil {
		writeOrganizationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "organizationID"))
	target, parseErr := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil || parseErr != nil {
		writeOrganizationError(w, ErrMemberNotFound)
		return
	}
	if err = h.service.RemoveMember(r.Context(), userID, id, target); err != nil {
		writeOrganizationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if json.NewDecoder(r.Body).Decode(target) != nil {
		writeOrganizationError(w, ErrInvalidInput)
		return false
	}
	return true
}
func writeOrganizationJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeOrganizationError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "An unexpected error occurred."
	switch {
	case errors.Is(err, ErrInvalidInput):
		status, code, message = http.StatusBadRequest, "invalid_request", "Request data is invalid."
	case errors.Is(err, ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "You do not have permission for this action."
	case errors.Is(err, ErrNotFound):
		status, code, message = http.StatusNotFound, "organization_not_found", "Organization not found."
	case errors.Is(err, ErrMemberNotFound):
		status, code, message = http.StatusNotFound, "member_not_found", "Member not found."
	case errors.Is(err, ErrUserNotFound):
		status, code, message = http.StatusNotFound, "user_not_found", "User not found."
	case errors.Is(err, ErrSlugTaken):
		status, code, message = http.StatusConflict, "slug_taken", "That slug is already in use."
	case errors.Is(err, ErrDuplicateMembership):
		status, code, message = http.StatusConflict, "duplicate_membership", "That user is already a member."
	case errors.Is(err, ErrInvalidRole):
		status, code, message = http.StatusBadRequest, "invalid_role", "That role is invalid."
	case errors.Is(err, ErrLastOwner):
		status, code, message = http.StatusConflict, "last_owner", "An organization must retain an owner."
	}
	writeOrganizationJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
