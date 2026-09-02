package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	userID := uuid.New()
	token, err := createAccessToken(userID, "test-secret", time.Minute)
	if err != nil {
		t.Fatalf("createAccessToken() error = %v", err)
	}
	got, err := parseAccessToken(token, "test-secret")
	if err != nil {
		t.Fatalf("parseAccessToken() error = %v", err)
	}
	if got != userID {
		t.Fatalf("user ID = %v, want %v", got, userID)
	}
}

func TestAccessTokenRejectsInvalidAndExpiredTokens(t *testing.T) {
	userID := uuid.New()
	token, err := createAccessToken(userID, "test-secret", -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseAccessToken(token, "test-secret"); err == nil {
		t.Fatal("expired token was accepted")
	}
	if _, err := parseAccessToken(token, "wrong-secret"); err == nil {
		t.Fatal("token signed with another secret was accepted")
	}
}

func TestRefreshTokenIsRandomAndHashed(t *testing.T) {
	first, err := createRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := createRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("refresh tokens were repeated")
	}
	if hashRefreshToken(first) == first {
		t.Fatal("refresh token was not hashed")
	}
}
