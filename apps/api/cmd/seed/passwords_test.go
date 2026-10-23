package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"flowcart/apps/api/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func setProductionPasswords(t *testing.T) seedPasswords {
	t.Helper()
	// Synthetic test fixtures, never hosted credentials.
	passwords := seedPasswords{"test-owner-private-password-2026", "test-manager-private-password-2026", "test-viewer-private-password-2026"}
	t.Setenv("FLOWCART_SEED_OWNER_PASSWORD", passwords.owner)
	t.Setenv("FLOWCART_SEED_MANAGER_PASSWORD", passwords.manager)
	t.Setenv("FLOWCART_SEED_VIEWER_PASSWORD", passwords.viewer)
	return passwords
}

func TestSeedRejectsProductionAndStagingBeforeDatabaseConnection(t *testing.T) {
	for _, env := range []string{"production", "staging"} {
		t.Setenv("APP_ENV", env)
		t.Setenv("FLOWCART_SEED_CONFIRM", "FLOWCART_DEMO")
		t.Setenv("DATABASE_URL", "postgres://flowcart:ignored@localhost:5433/flowcart?sslmode=disable")
		if err := run(); err == nil || !strings.Contains(err.Error(), "refusing to seed") {
			t.Fatalf("APP_ENV=%s was not rejected before connection", env)
		}
	}
}

func TestSeedPasswordsAllowOnlySafeEnvironments(t *testing.T) {
	for _, env := range []string{"development", "test", "verification"} {
		got, err := loadSeedPasswords(env)
		if err != nil || got != (seedPasswords{demoPassword, demoPassword, demoPassword}) {
			t.Fatal("local demo defaults changed")
		}
	}
	for _, env := range []string{"production", "staging", "prodution"} {
		if _, err := loadSeedPasswords(env); err == nil {
			t.Fatalf("APP_ENV=%s accepted seed passwords", env)
		}
	}
}

func TestProductionSeedPasswordsAreHashedAndIdempotent(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal("connect test database failed")
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin test transaction failed")
	}
	defer tx.Rollback(ctx)
	passwords := setProductionPasswords(t)
	hashes, err := hashSeedPasswords(passwords)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.VerifyPassword(passwords.owner, hashes.owner) || auth.VerifyPassword(demoPassword, hashes.owner) {
		t.Fatal("owner password hash did not enforce private credentials")
	}
	data := makeSeedData()
	for i := 0; i < 2; i++ {
		if err := seedBase(ctx, tx, data, hashes); err != nil {
			t.Fatal(err)
		}
		if err := verifySeedPasswords(ctx, tx, data, passwords); err != nil {
			t.Fatal(err)
		}
	}
	defaultHash, err := auth.HashPassword(demoPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash=$1 WHERE id=$2`, defaultHash, data.owner); err != nil {
		t.Fatal(err)
	}
	if err := seedBase(ctx, tx, data, hashes); err != nil {
		t.Fatal(err)
	}
	if err := verifySeedPasswords(ctx, tx, data, passwords); err == nil {
		t.Fatal("existing development default was accepted for production")
	}
}
