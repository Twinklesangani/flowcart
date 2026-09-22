package config

import (
	"strings"
	"testing"
)

func configEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"PORT", "APP_ENV", "CORS_ALLOWED_ORIGINS", "ACCESS_TOKEN_TTL", "REFRESH_TOKEN_TTL", "PAYMENTS_PROVIDER", "STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET", "STRIPE_EXPECTED_LIVEMODE"} {
		t.Setenv(key, "")
	}
	t.Setenv("DATABASE_URL", "postgres://localhost/unused")
	t.Setenv("JWT_SECRET", "test-secret")
}

func TestConfigSecurity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		env       map[string]string
		wantError bool
	}{
		{"local defaults", nil, false},
		{"verification", map[string]string{"APP_ENV": "verification"}, false},
		{"test", map[string]string{"APP_ENV": "test"}, false},
		{"unknown", map[string]string{"APP_ENV": "prodution"}, true},
		{"zero", map[string]string{"ACCESS_TOKEN_TTL": "0s"}, true},
		{"negative", map[string]string{"REFRESH_TOKEN_TTL": "-1h"}, true},
		{"invalid", map[string]string{"ACCESS_TOKEN_TTL": "nope"}, true},
		{"equal", map[string]string{"ACCESS_TOKEN_TTL": "1h", "REFRESH_TOKEN_TTL": "1h"}, true},
		{"short refresh", map[string]string{"ACCESS_TOKEN_TTL": "2h", "REFRESH_TOKEN_TTL": "1h"}, true},
		{"valid TTL", map[string]string{"ACCESS_TOKEN_TTL": "10m", "REFRESH_TOKEN_TTL": "24h"}, false},
		{"wildcard", map[string]string{"CORS_ALLOWED_ORIGINS": "*"}, true},
		{"stripe missing secrets", map[string]string{"PAYMENTS_PROVIDER": "stripe"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestStripeMissingKeyFailsConfiguration(t *testing.T) {
	configEnv(t)
	t.Setenv("PAYMENTS_PROVIDER", "stripe")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_test")
	if _, err := Load(); err == nil {
		t.Fatal("stripe configuration accepted a missing API key")
	}
}

func TestStripeMissingWebhookSecretFailsConfiguration(t *testing.T) {
	configEnv(t)
	t.Setenv("PAYMENTS_PROVIDER", "stripe")
	t.Setenv("STRIPE_SECRET_KEY", "sk_test")
	if _, err := Load(); err == nil {
		t.Fatal("stripe configuration accepted a missing webhook secret")
	}
}

func TestDisabledProviderStartsConfiguration(t *testing.T) {
	configEnv(t)
	t.Setenv("PAYMENTS_PROVIDER", "disabled")
	if _, err := Load(); err != nil {
		t.Fatalf("disabled provider configuration failed: %v", err)
	}
}

func TestProductionSecretAndOrigins(t *testing.T) {
	// Synthetic non-secret fixture; production operators must use random bytes.
	strong := "S3-fixture-0123456789-ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	for _, tc := range []struct {
		name, secret, origins string
		fail                  bool
	}{
		{"empty", "", "https://app.example.com", true},
		{"short", "tiny", "https://app.example.com", true},
		{"example", "replace-with-a-long-development-secret", "https://app.example.com", true},
		{"repeated", strings.Repeat("x", 64), "https://app.example.com", true},
		{"missing origins", strong, "", true},
		{"wildcard", strong, "https://*.example.com", true},
		{"HTTP", strong, "http://localhost:3000", true},
		{"valid", strong, " https://APP.example.com:443/ , https://other.example.com:8443", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configEnv(t)
			t.Setenv("APP_ENV", "production")
			t.Setenv("JWT_SECRET", tc.secret)
			t.Setenv("CORS_ALLOWED_ORIGINS", tc.origins)
			cfg, err := Load()
			if (err != nil) != tc.fail {
				t.Fatalf("unexpected validation result: %v", err)
			}
			if err != nil && tc.secret != "" && strings.Contains(err.Error(), tc.secret) {
				t.Fatal("secret leaked")
			}
			if !tc.fail && (!cfg.Origins.TrustedOrigin("https://app.example.com") || cfg.Origins.TrustedOrigin("https://evil-app.example.com") || cfg.Origins.TrustedOrigin("http://localhost:3000")) {
				t.Fatal("incorrect production origins")
			}
		})
	}
}
