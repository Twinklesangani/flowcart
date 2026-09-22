package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

type userRepository interface {
	CreateUser(context.Context, User) (User, error)
	FindUserByEmail(context.Context, string) (User, error)
	FindUserByID(context.Context, uuid.UUID) (User, error)
	CreateSession(context.Context, Session) error
	FindActiveSession(context.Context, string) (Session, error)
	RotateSession(context.Context, uuid.UUID, Session) error
	RevokeSession(context.Context, string) error
}

var ErrInvalidInput = errors.New("invalid input")
var ErrInvalidCredentials = errors.New("invalid credentials")

type Service struct {
	repository userRepository
	jwtSecret  string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewService(repository userRepository, jwtSecret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{repository: repository, jwtSecret: jwtSecret, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (service *Service) Register(ctx context.Context, email, password, firstName, lastName string) (AuthResult, error) {
	email, err := normalizeEmail(email)
	if err != nil || !validPassword(password) {
		return AuthResult{}, ErrInvalidInput
	}
	passwordHash, err := hashForRegistration(password)
	if err != nil {
		return AuthResult{}, err
	}
	user, err := service.repository.CreateUser(ctx, User{Email: email, PasswordHash: passwordHash, FirstName: strings.TrimSpace(firstName), LastName: strings.TrimSpace(lastName)})
	if err != nil {
		return AuthResult{}, err
	}
	return service.issueTokens(ctx, user)
}

func (service *Service) Login(ctx context.Context, email, password string) (AuthResult, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return AuthResult{}, ErrInvalidCredentials
	}
	user, err := service.repository.FindUserByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		return AuthResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return AuthResult{}, fmt.Errorf("login user lookup: %w", err)
	}
	valid, err := verifyForLogin(password, user.PasswordHash)
	if err != nil {
		return AuthResult{}, err
	}
	if !valid {
		return AuthResult{}, ErrInvalidCredentials
	}
	return service.issueTokens(ctx, user)
}

func (service *Service) Refresh(ctx context.Context, refreshToken string) (AuthResult, error) {
	if refreshToken == "" {
		return AuthResult{}, ErrInvalidCredentials
	}
	session, err := service.repository.FindActiveSession(ctx, hashRefreshToken(refreshToken))
	if errors.Is(err, ErrNotFound) {
		return AuthResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return AuthResult{}, fmt.Errorf("refresh session lookup: %w", err)
	}
	user, err := service.repository.FindUserByID(ctx, session.UserID)
	if errors.Is(err, ErrNotFound) {
		return AuthResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return AuthResult{}, fmt.Errorf("refresh user lookup: %w", err)
	}
	return service.rotateTokens(ctx, session, user)
}

func (service *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return service.repository.RevokeSession(ctx, hashRefreshToken(refreshToken))
}

func (service *Service) CurrentUser(ctx context.Context, userID uuid.UUID) (User, error) {
	return service.repository.FindUserByID(ctx, userID)
}

func (service *Service) issueTokens(ctx context.Context, user User) (AuthResult, error) {
	accessToken, err := createAccessToken(user.ID, service.jwtSecret, service.accessTTL)
	if err != nil {
		return AuthResult{}, err
	}
	return service.createSession(ctx, user, accessToken)
}

func (service *Service) rotateTokens(ctx context.Context, oldSession Session, user User) (AuthResult, error) {
	accessToken, err := createAccessToken(user.ID, service.jwtSecret, service.accessTTL)
	if err != nil {
		return AuthResult{}, err
	}
	refreshToken, err := createRefreshToken()
	if err != nil {
		return AuthResult{}, err
	}
	expiresAt := sessionExpiry(time.Now(), service.refreshTTL)
	replacement := Session{ID: uuid.New(), UserID: user.ID, RefreshTokenHash: hashRefreshToken(refreshToken), ExpiresAt: expiresAt}
	if err := service.repository.RotateSession(ctx, oldSession.ID, replacement); err != nil {
		if errors.Is(err, ErrNotFound) {
			return AuthResult{}, ErrInvalidCredentials
		}
		return AuthResult{}, fmt.Errorf("rotate refresh session: %w", err)
	}
	return AuthResult{User: user, AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: expiresAt}, nil
}

func (service *Service) createSession(ctx context.Context, user User, accessToken string) (AuthResult, error) {
	refreshToken, err := createRefreshToken()
	if err != nil {
		return AuthResult{}, err
	}
	expiresAt := sessionExpiry(time.Now(), service.refreshTTL)
	if err := service.repository.CreateSession(ctx, Session{ID: uuid.New(), UserID: user.ID, RefreshTokenHash: hashRefreshToken(refreshToken), ExpiresAt: expiresAt}); err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: user, AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: expiresAt}, nil
}

func normalizeEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return "", ErrInvalidInput
	}
	return email, nil
}

func validPassword(password string) bool { return len(password) >= 8 && len(password) <= 128 }
