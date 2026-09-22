package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuthRejectsBodiesBeforeService(t *testing.T) {
	// A nil service makes any accidental service invocation fail this test.
	h := NewHandler(nil, false)
	for name, handler := range map[string]http.HandlerFunc{"login": h.Login, "register": h.Register} {
		for _, tc := range []struct {
			name, body, contentType string
			status                  int
		}{
			{"oversized", `{"email":"user@example.com","password":"password123","first_name":"` + strings.Repeat("x", 16*1024) + `"}`, "application/json", 413},
			{"oversized trailing whitespace", `{"email":"user@example.com","password":"password123"}` + strings.Repeat(" ", 16*1024), "application/json", 413},
			{"plain", `{}`, "text/plain", 415},
			{"form", `{}`, "application/x-www-form-urlencoded", 415},
			{"trailing", `{} garbage`, "application/json", 400},
			{"multiple", `{} {}`, "application/json", 400},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
				r.ContentLength = -1
				r.Header.Set("Content-Type", tc.contentType)
				w := httptest.NewRecorder()
				handler(w, r)
				if w.Code != tc.status {
					t.Fatalf("got %d want %d", w.Code, tc.status)
				}
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("missing no-store")
				}
			})
		}
	}
}

func TestAuthNormalLoginContentTypes(t *testing.T) {
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	user := User{ID: uuid.New(), Email: "user@example.com", PasswordHash: hash}
	repo := &fakeRepository{users: map[string]User{user.Email: user}, byID: map[uuid.UUID]User{user.ID: user}}
	h := NewHandler(NewService(repo, "test-secret", time.Minute, time.Hour), false)
	for _, contentType := range []string{"application/json", "application/json; charset=utf-8"} {
		r := httptest.NewRequest("POST", "/login", strings.NewReader(`{"email":"user@example.com","password":"password123"}`))
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		h.Login(w, r)
		if w.Code != 200 || len(w.Result().Cookies()) != 1 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("login: %d %s", w.Code, w.Body)
		}
	}
}

func TestAuthMutationOrigins(t *testing.T) {
	repo := &fakeRepository{users: map[string]User{}, byID: map[uuid.UUID]User{}}
	h := NewHandler(NewService(repo, "test-secret", time.Minute, time.Hour), false)
	for name, handler := range map[string]http.HandlerFunc{"login": h.Login, "register": h.Register, "refresh": h.Refresh, "logout": h.Logout} {
		for _, origin := range []string{"absent", "http://localhost:3000", "http://localhost:3001", "https://untrusted.example", "null", ""} {
			t.Run(name+"/"+origin, func(t *testing.T) {
				r := httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
				r.Header.Set("Content-Type", "application/json")
				if origin != "absent" {
					r.Header.Set("Origin", origin)
				}
				w := httptest.NewRecorder()
				handler(w, r)
				want := 403
				if origin == "absent" || strings.HasPrefix(origin, "http://localhost:") {
					want = map[string]int{"login": 401, "register": 400, "refresh": 401, "logout": 204}[name]
				}
				if w.Code != want {
					t.Fatalf("got %d want %d", w.Code, want)
				}
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("missing no-store")
				}
			})
		}
	}
}
