package auth

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepository struct {
	users map[string]User
	byID  map[uuid.UUID]User
}

func (repository *fakeRepository) CreateUser(_ context.Context, user User) (User, error) {
	if _, ok := repository.users[user.Email]; ok {
		return User{}, ErrDuplicateEmail
	}
	user.ID = uuid.New()
	repository.users[user.Email] = user
	repository.byID[user.ID] = user
	return user, nil
}
func (repository *fakeRepository) FindUserByEmail(_ context.Context, email string) (User, error) {
	user, ok := repository.users[email]
	if !ok {
		return User{}, ErrNotFound
	}
	return user, nil
}
func (repository *fakeRepository) FindUserByID(_ context.Context, id uuid.UUID) (User, error) {
	user, ok := repository.byID[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return user, nil
}
func (repository *fakeRepository) CreateSession(context.Context, Session) error { return nil }
func (repository *fakeRepository) FindActiveSession(context.Context, string) (Session, error) {
	return Session{}, ErrNotFound
}
func (repository *fakeRepository) RotateSession(context.Context, uuid.UUID, Session) error {
	return nil
}
func (repository *fakeRepository) RevokeSession(context.Context, string) error { return nil }

func TestRegisterNormalizesEmailAndHashesPassword(t *testing.T) {
	repository := &fakeRepository{users: map[string]User{}, byID: map[uuid.UUID]User{}}
	service := NewService(repository, "test-secret", time.Minute, time.Hour)
	result, err := service.Register(context.Background(), " User@Example.com ", "strong password", "A", "User")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.User.Email != "user@example.com" {
		t.Fatalf("email = %q", result.User.Email)
	}
	if result.User.PasswordHash == "strong password" || !VerifyPassword("strong password", result.User.PasswordHash) {
		t.Fatal("password was not hashed correctly")
	}
}

func TestRegisterRejectsInvalidInput(t *testing.T) {
	repository := &fakeRepository{users: map[string]User{}, byID: map[uuid.UUID]User{}}
	service := NewService(repository, "test-secret", time.Minute, time.Hour)
	if _, err := service.Register(context.Background(), "not-an-email", "short", "", ""); err != ErrInvalidInput {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestLoginUsesGenericInvalidCredentialsError(t *testing.T) {
	repository := &fakeRepository{users: map[string]User{}, byID: map[uuid.UUID]User{}}
	service := NewService(repository, "test-secret", time.Minute, time.Hour)
	if _, err := service.Login(context.Background(), "unknown@example.com", "password"); err != ErrInvalidCredentials {
		t.Fatalf("error = %v, want ErrInvalidCredentials", err)
	}
}
