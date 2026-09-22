package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func limitRequest(h http.HandlerFunc, ip, email, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	r := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
	r.RemoteAddr = ip + ":1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Forwarded-For", uuid.NewString())
	r.Header.Set("X-Real-IP", uuid.NewString())
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func TestLoginIPAccountLimitsAndRecovery(t *testing.T) {
	now := time.Unix(100, 0)
	h := NewHandler(NewService(&fakeRepository{}, "test-secret", time.Minute, time.Hour), false)
	h.limits = newAuthLimits(func() time.Time { return now })
	for i := 0; i < 10; i++ {
		if w := limitRequest(h.Login, "192.0.2.1", fmt.Sprintf("u%d@example.com", i), "bad"); w.Code != 401 {
			t.Fatalf("under IP limit: %d", w.Code)
		}
	}
	if w := limitRequest(h.Login, "192.0.2.1", "new@example.com", "bad"); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("IP limit missing")
	}
	if w := limitRequest(h.Login, "192.0.2.2", "new@example.com", "bad"); w.Code != 401 {
		t.Fatal("distinct IP denied")
	}
	for i := 0; i < 5; i++ {
		email := " victim@example.com "
		if i%2 == 0 {
			email = "VICTIM@EXAMPLE.COM"
		}
		if w := limitRequest(h.Login, fmt.Sprintf("198.51.100.%d", i), email, "bad"); w.Code != 401 {
			t.Fatalf("account under limit: %d", w.Code)
		}
	}
	if w := limitRequest(h.Login, "203.0.113.1", "victim@example.com", "bad"); w.Code != 429 {
		t.Fatal("normalized account bypass")
	}
	now = now.Add(time.Minute)
	if w := limitRequest(h.Login, "192.0.2.1", "victim@example.com", "bad"); w.Code != 401 {
		t.Fatal("account permanently locked")
	}
}

func TestRegistrationAndRefreshLimits(t *testing.T) {
	now := time.Unix(100, 0)
	repo := &fakeRepository{users: map[string]User{}, byID: map[uuid.UUID]User{}}
	h := NewHandler(NewService(repo, "test-secret", time.Minute, time.Hour), false)
	h.limits = newAuthLimits(func() time.Time { return now })
	for i := 0; i < 5; i++ {
		want := 409
		if i == 0 {
			want = 201
		}
		if w := limitRequest(h.Register, "192.0.2.1", "user@example.com", "password123"); w.Code != want {
			t.Fatalf("register=%d want=%d", w.Code, want)
		}
	}
	if w := limitRequest(h.Register, "192.0.2.1", "user@example.com", "password123"); w.Code != 429 {
		t.Fatal("registration limit missing")
	}
	now = now.Add(2 * time.Minute)
	if w := limitRequest(h.Register, "192.0.2.1", "user@example.com", "password123"); w.Code != 409 {
		t.Fatal("registration refill failed")
	}
	for i := 0; i < 60; i++ {
		if w := limitRequest(h.Refresh, "192.0.2.1", "", ""); w.Code != 401 {
			t.Fatalf("ordinary refresh attempt denied at %d", i)
		}
	}
	if w := limitRequest(h.Refresh, "192.0.2.1", "", ""); w.Code != 429 {
		t.Fatal("refresh limit missing")
	}
	now = now.Add(time.Second)
	if w := limitRequest(h.Refresh, "192.0.2.1", "", ""); w.Code != 401 {
		t.Fatal("refresh refill failed")
	}
}

func TestSuccessfulLoginRefundsAccountBudget(t *testing.T) {
	hash, _ := HashPassword("password123")
	user := User{ID: uuid.New(), Email: "user@example.com", PasswordHash: hash}
	h := NewHandler(NewService(&fakeRepository{users: map[string]User{user.Email: user}}, "test-secret", time.Minute, time.Hour), false)
	for i := 0; i < 7; i++ {
		if w := limitRequest(h.Login, "192.0.2.1", user.Email, "password123"); w.Code != 200 {
			t.Fatalf("successful login consumed failure budget: %d", w.Code)
		}
	}
}

func TestTemporaryLoginErrorsRefundAccountBudget(t *testing.T) {
	h := NewHandler(NewService(&failingAuthRepository{err: errors.New("database unavailable")}, "test-secret", time.Minute, time.Hour), false)
	for i := 0; i < 7; i++ {
		if w := limitRequest(h.Login, "192.0.2.1", "user@example.com", "password123"); w.Code != 500 {
			t.Fatalf("internal error consumed failure budget: %d", w.Code)
		}
	}
}

func TestRefreshNormalRepeatedSuccess(t *testing.T) {
	h := NewHandler(NewService(&failingAuthRepository{}, "test-secret", time.Minute, time.Hour), false)
	for i := 0; i < 10; i++ {
		r := httptest.NewRequest("POST", "/refresh", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "test-refresh"})
		w := httptest.NewRecorder()
		h.Refresh(w, r)
		if w.Code != 200 || len(w.Result().Cookies()) != 1 {
			t.Fatalf("refresh denied: %d", w.Code)
		}
	}
}

func TestArgon2BudgetSharedAndNoWaitingQueue(t *testing.T) {
	if cap(passwordWork) != 2 || len(passwordWork) != 0 {
		t.Fatal("unexpected process budget")
	}
	passwordWork <- struct{}{}
	passwordWork <- struct{}{}
	func() {
		defer func() { <-passwordWork; <-passwordWork }()
		for i := 0; i < 2; i++ {
			repo := &fakeRepository{users: map[string]User{"user@example.com": {Email: "user@example.com", PasswordHash: "unused"}}}
			s := NewService(repo, "test-secret", time.Minute, time.Hour)
			if _, err := s.Register(context.Background(), "new@example.com", "password123", "", ""); !errors.Is(err, ErrHashCapacity) {
				t.Fatalf("registration budget: %v", err)
			}
			if _, err := s.Login(context.Background(), "user@example.com", "password123"); !errors.Is(err, ErrHashCapacity) {
				t.Fatalf("login budget: %v", err)
			}
			w := httptest.NewRecorder()
			NewHandler(s, false).respondServiceError(w, ErrHashCapacity)
			if w.Code != 429 || w.Header().Get("Retry-After") == "" {
				t.Fatal("budget response missing")
			}
		}
	}()
	if _, err := hashForRegistration("password123"); err != nil {
		t.Fatal("budget did not recover")
	}
	if len(passwordWork) != 0 {
		t.Fatal("permit leak")
	}
}
