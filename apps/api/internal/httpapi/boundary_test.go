package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"flowcart/apps/api/internal/auth"
)

func TestAuthRouterBoundary(t *testing.T) {
	t.Setenv("PAYMENTS_PROVIDER", "disabled")
	router, err := NewRouter(nil, auth.NewService(nil, "test-secret", time.Minute, time.Hour), "development")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path, origin, contentType, body string
		status                                  int
	}{
		{"GET", "me", "", "", "", 401},
		{"POST", "logout", "", "", "", 204},
		{"POST", "refresh", "", "", "", 401},
		{"POST", "login", "", "text/plain", `{}`, 415},
		{"POST", "register", "", "application/json", `{}` + strings.Repeat(" ", 16*1024), 413},
		{"POST", "login", "https://untrusted.example", "application/json", `{}`, 403},
		{"POST", "register", "https://untrusted.example", "application/json", `{}`, 403},
		{"POST", "refresh", "https://untrusted.example", "", "", 403},
		{"POST", "logout", "https://untrusted.example", "", "", 403},
	} {
		t.Run(tc.path+"/"+tc.origin+"/"+tc.contentType, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/api/v1/auth/"+tc.path, strings.NewReader(tc.body))
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d cache=%q", w.Code, w.Header().Get("Cache-Control"))
			}
		})
	}
}
