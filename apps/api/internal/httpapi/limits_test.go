package httpapi

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"flowcart/apps/api/internal/auth"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func userToken(t *testing.T, id uuid.UUID) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": id.String(), "type": "access", "exp": time.Now().Add(time.Minute).Unix()}).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestOrganizationCreationUserBudget(t *testing.T) {
	t.Setenv("PAYMENTS_PROVIDER", "disabled")
	router, err := NewRouter(nil, auth.NewService(nil, "test-secret", time.Minute, time.Hour), "development")
	if err != nil {
		t.Fatal(err)
	}
	token := userToken(t, uuid.New())
	for i := 0; i < 6; i++ {
		r := httptest.NewRequest("POST", "/api/v1/organizations", strings.NewReader(`{}`))
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", i)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		want := 400
		if i == 5 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("got=%d want=%d", w.Code, want)
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/organizations", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer "+userToken(t, uuid.New()))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("second user budget shared")
	}
}

func TestExpensiveRoutesShareUserBudgetAndReadsRemainAvailable(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	org, a, b := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{a, b} {
		if _, err = pool.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test-only')`, id, id.String()+"@example.com"); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, org)
		pool.Exec(ctx, `DELETE FROM users WHERE id=ANY($1)`, []uuid.UUID{a, b})
	})
	if _, err = pool.Exec(ctx, `INSERT INTO organizations(id,name,slug) VALUES($1,'S2 verification',$2)`, org, org.String()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{a, b} {
		if _, err = pool.Exec(ctx, `INSERT INTO organization_members(organization_id,user_id,role) VALUES($1,$2,'owner')`, org, id); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PAYMENTS_PROVIDER", "disabled")
	router, err := NewRouter(pool, auth.NewService(auth.NewRepository(pool), "test-secret", time.Minute, time.Hour), "development")
	if err != nil {
		t.Fatal(err)
	}
	prefix := "/api/v1/organizations/" + org.String()
	id := uuid.NewString()
	paths := []string{"/orders", "/orders/auto-allocate", "/orders/" + id + "/payments", "/orders/" + id + "/checkout", "/inventory/" + id + "/adjust", "/transfers", "/transfers/" + id + "/dispatch", "/transfers/" + id + "/receive"}
	call := func(method, path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, prefix+path, strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	token := userToken(t, a)
	for i := 0; i < 30; i++ {
		w := call("POST", paths[i%len(paths)], token)
		if w.Code == 429 || w.Code == 500 {
			t.Fatalf("unexpected under-limit response: %d %s", w.Code, w.Body)
		}
	}
	for _, path := range paths {
		w := call("POST", path, token)
		if w.Code != 429 || w.Header().Get("Retry-After") == "" {
			t.Fatalf("unlimited route %s: %d", path, w.Code)
		}
	}
	if w := call("GET", "", token); w.Code != 200 {
		t.Fatalf("read throttled: %d", w.Code)
	}
	if w := call("POST", "/orders", userToken(t, b)); w.Code != 400 {
		t.Fatalf("other user throttled: %d", w.Code)
	}
}
