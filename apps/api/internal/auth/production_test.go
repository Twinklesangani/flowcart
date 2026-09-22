package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestAccessTokenRequiresExpirationAndAlgorithm(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exp    any
		kind   string
		method jwt.SigningMethod
		valid  bool
	}{
		{"valid", time.Now().Add(time.Minute).Unix(), "access", jwt.SigningMethodHS256, true},
		{"missing", nil, "access", jwt.SigningMethodHS256, false},
		{"expired", time.Now().Add(-time.Minute).Unix(), "access", jwt.SigningMethodHS256, false},
		{"malformed", "tomorrow", "access", jwt.SigningMethodHS256, false},
		{"algorithm", time.Now().Add(time.Minute).Unix(), "access", jwt.SigningMethodHS384, false},
		{"type", time.Now().Add(time.Minute).Unix(), "refresh", jwt.SigningMethodHS256, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwt.MapClaims{"sub": uuid.NewString(), "type": tc.kind}
			if tc.exp != nil {
				claims["exp"] = tc.exp
			}
			value, err := jwt.NewWithClaims(tc.method, claims).SignedString([]byte("test-secret"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = parseAccessToken(value, "test-secret")
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}

func TestProductionRegistrationIsUnavailable(t *testing.T) {
	handler := NewHandlerForEnvironment(nil, true, false)
	request := httptest.NewRequest("POST", "/register", strings.NewReader(`{"email":"new@example.com","password":"password123"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.Register(response, request)
	if response.Code != 404 {
		t.Fatalf("registration status=%d, want 404", response.Code)
	}
}
func TestRefreshCookieEnvironmentSettings(t *testing.T) {
	for _, production := range []bool{false, true} {
		h := NewHandler(nil, production)
		w := httptest.NewRecorder()
		h.writeAuthResponse(w, 200, AuthResult{RefreshToken: "test-only", ExpiresAt: time.Now().Add(time.Hour)})
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("missing cookie")
		}
		c := cookies[0]
		if !c.HttpOnly || c.Secure != production || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge <= 0 || c.Expires.IsZero() || c.Domain != "" {
			t.Fatal("unsafe cookie attributes")
		}
		w = httptest.NewRecorder()
		h.clearRefreshCookie(w)
		c = w.Result().Cookies()[0]
		if c.MaxAge != -1 || c.Secure != production || !c.HttpOnly {
			t.Fatal("unsafe clearing cookie")
		}
	}
}
