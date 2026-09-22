package organization

import (
	"flowcart/apps/api/internal/auth"
	"net/http"

	"github.com/google/uuid"
)

func Membership(service *Service, organizationID func(*http.Request) (uuid.UUID, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.UserIDFromContext(r.Context())
			if !ok {
				writeUnauthorized(w)
				return
			}
			id, err := organizationID(r)
			if err != nil {
				writeOrganizationError(w, ErrNotFound)
				return
			}
			membership, err := service.Membership(r.Context(), id, userID)
			if err != nil {
				writeOrganizationError(w, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(withTenant(r.Context(), TenantContext{OrganizationID: id, UserID: userID, Role: membership.Role})))
		})
	}
}
func writeUnauthorized(w http.ResponseWriter) {
	writeOrganizationJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "unauthorized", "message": "Authentication is required."}})
}
