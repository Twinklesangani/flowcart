package httpapi

import (
	"flowcart/apps/api/internal/auth"
	"flowcart/apps/api/internal/httpboundary"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProductionCORSAuthAndSecurityHeaders(t *testing.T) {
	t.Setenv("PAYMENTS_PROVIDER", "disabled")
	policy, err := httpboundary.NewOriginPolicy("https://app.example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(nil, auth.NewService(nil, "test-secret", time.Minute, time.Hour), "production", policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://app.example.com", "https://evil-app.example.com", "http://localhost:3000"} {
		for _, method := range []string{"OPTIONS", "POST"} {
			r := httptest.NewRequest(method, "/api/v1/auth/login", strings.NewReader(`{}`))
			r.Header.Set("Origin", origin)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			trusted := origin == "https://app.example.com"
			if (w.Header().Get("Access-Control-Allow-Origin") == origin) != trusted {
				t.Fatal("CORS policy mismatch")
			}
			if trusted && w.Header().Get("Access-Control-Allow-Credentials") != "true" {
				t.Fatal("credentials missing")
			}
			want := 204
			if method == "POST" {
				want = 403
				if trusted {
					want = 401
				}
			}
			if w.Code != want {
				t.Fatalf("method=%s status=%d want=%d", method, w.Code, want)
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Header().Get("X-Frame-Options") != "DENY" {
				t.Fatal("security headers missing")
			}
		}
	}
	r := httptest.NewRequest("GET", "/missing", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 404 || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("global headers missing on 404")
	}
}

func TestProductionProvisioningRoutesAreBlocked(t *testing.T) {
	policy, err := httpboundary.NewOriginPolicy("https://app.example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	router, err := NewRouter(nil, auth.NewService(nil, "test-secret", time.Minute, time.Hour), "production", policy)
	if err != nil {
		t.Fatal(err)
	}
	register := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(`{"email":"new@example.com","password":"password123"}`))
	register.Header.Set("Content-Type", "application/json")
	register.Header.Set("Origin", "https://app.example.com")
	registerResponse := httptest.NewRecorder()
	router.ServeHTTP(registerResponse, register)
	if registerResponse.Code != 404 {
		t.Fatalf("production registration status=%d, want 404", registerResponse.Code)
	}
	member := httptest.NewRequest("POST", "/api/v1/organizations/00000000-0000-0000-0000-000000000000/members", strings.NewReader(`{"email":"member@example.com","role":"viewer"}`))
	member.Header.Set("Content-Type", "application/json")
	memberResponse := httptest.NewRecorder()
	router.ServeHTTP(memberResponse, member)
	if memberResponse.Code != 401 {
		t.Fatalf("unauthenticated member addition status=%d, want 401", memberResponse.Code)
	}
}
