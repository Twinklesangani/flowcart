package auth

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type contextKey string

const userIDKey contextKey = "authenticated-user-id"

func (service *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		const prefix = "Bearer "
		header := request.Header.Get("Authorization")
		if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
			writeUnauthorized(writer)
			return
		}
		userID, err := parseAccessToken(header[len(prefix):], service.jwtSecret)
		if err != nil {
			writeUnauthorized(writer)
			return
		}
		contextWithUser := context.WithValue(request.Context(), userIDKey, userID)
		next.ServeHTTP(writer, request.WithContext(contextWithUser))
	})
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(userIDKey).(uuid.UUID)
	return userID, ok
}

func writeUnauthorized(writer http.ResponseWriter) {
	writeError(writer, http.StatusUnauthorized, "unauthorized", "Authentication is required.")
}
