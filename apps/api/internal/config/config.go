package config

import (
	"errors"
	"os"
	"strings"
	"time"

	"flowcart/apps/api/internal/httpboundary"
)

type Config struct {
	Port                   string
	DatabaseURL            string
	JWTSecret              string
	AccessTokenTTL         time.Duration
	RefreshTokenTTL        time.Duration
	AppEnv                 string
	Origins                httpboundary.OriginPolicy
	PaymentsProvider       string
	StripeSecretKey        string
	StripeWebhookSecret    string
	StripeExpectedLivemode bool
}

func Load() (Config, error) {
	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if appEnv == "" {
		appEnv = "development"
	}
	switch appEnv {
	case "development", "test", "verification", "production":
	default:
		return Config{}, errors.New("APP_ENV must be development, test, verification or production")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}
	if appEnv == "production" && !strongProductionSecret(jwtSecret) {
		return Config{}, errors.New("JWT_SECRET must be a cryptographically random production secret of at least 32 bytes, not an example or default")
	}
	accessTokenTTL, err := loadDuration("ACCESS_TOKEN_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	refreshTokenTTL, err := loadDuration("REFRESH_TOKEN_TTL", 7*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	if refreshTokenTTL <= accessTokenTTL {
		return Config{}, errors.New("REFRESH_TOKEN_TTL must exceed ACCESS_TOKEN_TTL")
	}
	origins, err := httpboundary.NewOriginPolicy(os.Getenv("CORS_ALLOWED_ORIGINS"), appEnv == "production")
	if err != nil {
		return Config{}, err
	}
	paymentsProvider := os.Getenv("PAYMENTS_PROVIDER")
	if paymentsProvider == "" {
		paymentsProvider = "disabled"
	}
	if paymentsProvider != "disabled" && paymentsProvider != "stripe" {
		return Config{}, errors.New("PAYMENTS_PROVIDER must be disabled or stripe")
	}
	if paymentsProvider == "stripe" {
		if strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY")) == "" || strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET")) == "" {
			return Config{}, errors.New("STRIPE_SECRET_KEY and STRIPE_WEBHOOK_SECRET are required when PAYMENTS_PROVIDER=stripe")
		}
	}

	return Config{
		Port:                   port,
		DatabaseURL:            databaseURL,
		JWTSecret:              jwtSecret,
		AccessTokenTTL:         accessTokenTTL,
		RefreshTokenTTL:        refreshTokenTTL,
		AppEnv:                 appEnv,
		Origins:                origins,
		PaymentsProvider:       paymentsProvider,
		StripeSecretKey:        os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret:    os.Getenv("STRIPE_WEBHOOK_SECRET"),
		StripeExpectedLivemode: os.Getenv("STRIPE_EXPECTED_LIVEMODE") == "true",
	}, nil
}

func loadDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, errors.New(name + " must be a valid duration")
	}
	return duration, nil
}

// Length is not proof of entropy: operators must generate random secret material.
func strongProductionSecret(secret string) bool {
	if len(secret) < 32 || strings.TrimSpace(secret) != secret {
		return false
	}
	lower := strings.ToLower(secret)
	for _, marker := range []string{"replace", "change-me", "changeme", "example", "development", "test-secret", "default", "password"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	distinct := make(map[byte]bool)
	for i := range secret {
		distinct[secret[i]] = true
	}
	return len(distinct) >= 8
}
