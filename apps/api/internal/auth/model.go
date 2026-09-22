package auth

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	FirstName    string
	LastName     string
}

type Session struct {
	ID               uuid.UUID
	FamilyID         uuid.UUID
	UserID           uuid.UUID
	RefreshTokenHash string
	ExpiresAt        time.Time
	RevokedAt        *time.Time
}

type AuthResult struct {
	User         User
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}
