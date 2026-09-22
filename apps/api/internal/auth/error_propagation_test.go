package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type failingAuthRepository struct {
	fakeRepository
	stage string
	err   error
}

func (r *failingAuthRepository) FindUserByEmail(context.Context, string) (User, error) {
	return User{}, r.err
}
func (r *failingAuthRepository) FindActiveSession(context.Context, string) (Session, error) {
	if r.stage == "session" {
		return Session{}, r.err
	}
	return Session{ID: uuid.New(), UserID: uuid.New()}, nil
}
func (r *failingAuthRepository) FindUserByID(context.Context, uuid.UUID) (User, error) {
	if r.stage == "user" {
		return User{}, r.err
	}
	return User{ID: uuid.New()}, nil
}
func (r *failingAuthRepository) RotateSession(context.Context, uuid.UUID, Session) error {
	return r.err
}
func (r *failingAuthRepository) RevokeSession(context.Context, string) error { return r.err }

func TestAuthenticationDistinguishesMissingDataFromDatabaseFailure(t *testing.T) {
	dbErr := errors.New("database unavailable")
	for _, stage := range []string{"login", "session", "user", "rotation"} {
		for _, cause := range []error{fmt.Errorf("lookup: %w", ErrNotFound), dbErr} {
			t.Run(fmt.Sprintf("%s/%v", stage, cause), func(t *testing.T) {
				svc := NewService(&failingAuthRepository{stage: stage, err: cause}, "test-secret", time.Minute, time.Hour)
				var err error
				if stage == "login" {
					_, err = svc.Login(context.Background(), "user@example.com", "password")
				} else {
					_, err = svc.Refresh(context.Background(), "test-refresh")
				}
				want, status := cause, http.StatusInternalServerError
				if errors.Is(cause, ErrNotFound) {
					want, status = ErrInvalidCredentials, http.StatusUnauthorized
				}
				if !errors.Is(err, want) {
					t.Fatalf("error = %v, want %v", err, want)
				}
				response := httptest.NewRecorder()
				NewHandler(svc, false).respondServiceError(response, err)
				if response.Code != status {
					t.Fatalf("status = %d, want %d", response.Code, status)
				}
				if strings.Contains(response.Body.String(), dbErr.Error()) {
					t.Fatal("database details leaked in response")
				}
			})
		}
	}
}

func TestLogoutReportsRevocationFailureAndAllowsRetry(t *testing.T) {
	for _, fail := range []bool{false, true} {
		r := &failingAuthRepository{}
		if fail {
			r.err = errors.New("database unavailable")
		}
		h := NewHandler(NewService(r, "test-secret", time.Minute, time.Hour), false)
		request := httptest.NewRequest(http.MethodPost, "/logout", nil)
		request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "test-refresh"})
		response := httptest.NewRecorder()
		h.Logout(response, request)
		if fail {
			if response.Code != 500 || len(response.Result().Cookies()) != 0 {
				t.Fatal("failed revocation must return 500 and preserve the cookie for retry")
			}
		} else if response.Code != 204 || len(response.Result().Cookies()) != 1 {
			t.Fatal("successful logout must clear the cookie")
		}
	}
}
