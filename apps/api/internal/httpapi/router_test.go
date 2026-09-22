package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIdempotentWritePreflight(t *testing.T) {
	for _, origin := range []string{"http://localhost:3000", "http://localhost:3001", "https://untrusted.example"} {
		r := httptest.NewRequest(http.MethodOptions, "/api/v1/organizations/test/orders", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Access-Control-Request-Method", "POST")
		r.Header.Set("Access-Control-Request-Headers", "authorization,content-type,idempotency-key")
		w := httptest.NewRecorder()
		corsMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("preflight reached application handler") })).ServeHTTP(w, r)
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d", w.Code)
		}
		if origin == "https://untrusted.example" {
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("untrusted origin allowed")
			}
			continue
		}
		if w.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Fatal("allowed origin missing")
		}
		allowed := strings.ToLower(w.Header().Get("Access-Control-Allow-Headers"))
		for _, header := range []string{"authorization", "content-type", "idempotency-key"} {
			if !strings.Contains(allowed, header) {
				t.Fatalf("required header %s is blocked", header)
			}
		}
	}
}
