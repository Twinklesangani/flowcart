package main

import (
	"strings"
	"testing"
)

func TestSeedRequiresExplicitSafeEnvironment(t *testing.T) {
	for _, value := range []string{"production", "prod", "staging", "prodution", "", "unknown"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "invalid-do-not-connect")
			t.Setenv("FLOWCART_SEED_CONFIRM", "FLOWCART_DEMO")
			t.Setenv("APP_ENV", value)
			if err := run(); err == nil || !strings.Contains(err.Error(), "refusing to seed") {
				t.Fatal("unsafe seed was not rejected by the environment guard")
			}
			if safeSeedEnvironment(value) {
				t.Fatal("unsafe mode")
			}
		})
	}
	for _, value := range []string{"development", "test", "verification"} {
		if !safeSeedEnvironment(value) {
			t.Fatal("safe mode rejected")
		}
	}
	t.Setenv("DATABASE_URL", "invalid-do-not-connect")
	t.Setenv("APP_ENV", "development")
	t.Setenv("FLOWCART_SEED_CONFIRM", "")
	if err := run(); err == nil || !strings.Contains(err.Error(), "FLOWCART_SEED_CONFIRM") {
		t.Fatal("confirmation not required")
	}
	t.Setenv("FLOWCART_SEED_CONFIRM", "FLOWCART_DEMO")
	t.Setenv("DATABASE_URL", "")
	if err := run(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatal("missing database target was not rejected")
	}
}

func TestSeedDatabaseTargetAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name, env, target string
		allowed           bool
	}{
		{"development local", "development", "postgres://flowcart:ignored@localhost:5433/flowcart?sslmode=disable", true},
		{"test local", "test", "postgres://flowcart:ignored@127.0.0.1:5432/flowcart_test?sslmode=disable", true},
		{"verification local", "verification", "postgres://flowcart:ignored@[::1]:5432/flowcart_verification_20261002?sslmode=disable", true},
		{"S4 disposable verification local", "verification", "postgres://flowcart:ignored@localhost:5433/flowcart_s4_verify?sslmode=disable", true},
		{"production environment", "production", "postgres://flowcart:ignored@localhost:5433/flowcart?sslmode=disable", false},
		{"staging environment", "staging", "postgres://flowcart:ignored@localhost:5433/flowcart?sslmode=disable", false},
		{"remote production host", "development", "postgres://flowcart:ignored@db.production.example/flowcart?sslmode=disable", false},
		{"production database name", "development", "postgres://flowcart:ignored@localhost:5433/flowcart_production?sslmode=disable", false},
		{"wrong test database", "test", "postgres://flowcart:ignored@localhost:5433/flowcart?sslmode=disable", false},
		{"wrong verification database", "verification", "postgres://flowcart:ignored@localhost:5433/flowcart_test?sslmode=disable", false},
		{"missing URL", "development", "", false},
		{"malformed URL", "development", "not-a-postgres-url", false},
		{"typo environment", "prodution", "postgres://flowcart:ignored@localhost:5433/flowcart?sslmode=disable", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSeedDatabaseTarget(tc.env, tc.target)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v error=%v", tc.allowed, err)
			}
		})
	}
}
