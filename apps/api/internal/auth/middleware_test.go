package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthenticateRejectsMissingToken(t *testing.T) {
	service := NewService(nil, "test-secret", time.Minute, time.Hour)
	handler := service.Authenticate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	request := httptest.NewRequest(http.MethodGet, "/me", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
