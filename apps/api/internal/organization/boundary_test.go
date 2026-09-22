package organization

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"flowcart/apps/api/internal/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestOrdinaryMutationOversizedBeforeService(t *testing.T) {
	r := httptest.NewRequest("PATCH", "/organization", strings.NewReader(`{"name":"`+strings.Repeat("x", 64*1024)+`"}`))
	r.Header.Set("Content-Type", "application/json")
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": uuid.NewString(), "type": "access", "exp": time.Now().Add(time.Minute).Unix()}).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	// Authentication succeeds, but the nil organization service must never be called.
	auth.NewService(nil, "test-secret", time.Minute, time.Hour).Authenticate(http.HandlerFunc(NewHandler(nil).Create)).ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}
