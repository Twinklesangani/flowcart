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
	if err := run(); err == nil {
		t.Fatal("confirmation not required")
	}
}
