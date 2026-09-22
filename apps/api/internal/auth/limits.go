package auth

import (
	"errors"
	"time"

	"flowcart/apps/api/internal/ratelimit"
)

type authLimits struct {
	login, account, register, refresh *ratelimit.Limiter
}

func newAuthLimits(now func() time.Time) authLimits {
	return authLimits{
		login:    ratelimit.New(10, time.Minute, ratelimit.DefaultMaxEntries, now),
		account:  ratelimit.New(5, time.Minute, ratelimit.DefaultMaxEntries, now),
		register: ratelimit.New(5, 10*time.Minute, ratelimit.DefaultMaxEntries, now),
		refresh:  ratelimit.New(60, time.Minute, ratelimit.DefaultMaxEntries, now),
	}
}

var ErrHashCapacity = errors.New("password hashing capacity exhausted")

// Shared by every authentication Service in this process. No waiting queue.
var passwordWork = make(chan struct{}, 2)

func hashForRegistration(password string) (string, error) {
	select {
	case passwordWork <- struct{}{}:
		defer func() { <-passwordWork }()
		return HashPassword(password)
	default:
		return "", ErrHashCapacity
	}
}

func verifyForLogin(password, encoded string) (bool, error) {
	select {
	case passwordWork <- struct{}{}:
		defer func() { <-passwordWork }()
		return VerifyPassword(password, encoded), nil
	default:
		return false, ErrHashCapacity
	}
}
