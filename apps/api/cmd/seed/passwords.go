package main

import (
	"context"
	"errors"

	"flowcart/apps/api/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type seedPasswords struct {
	owner, manager, viewer string
}

func loadSeedPasswords(appEnv string) (seedPasswords, error) {
	if safeSeedEnvironment(appEnv) {
		return seedPasswords{demoPassword, demoPassword, demoPassword}, nil
	}
	return seedPasswords{}, errors.New("refusing to seed an unsupported environment")
}

func hashSeedPasswords(passwords seedPasswords) (seedPasswords, error) {
	var hashes seedPasswords
	for _, field := range []struct {
		password string
		hash     *string
	}{{passwords.owner, &hashes.owner}, {passwords.manager, &hashes.manager}, {passwords.viewer, &hashes.viewer}} {
		hash, err := auth.HashPassword(field.password)
		if err != nil {
			return seedPasswords{}, errors.New("hash seed password failed")
		}
		*field.hash = hash
	}
	return hashes, nil
}

// Reruns preserve existing credentials. Refuse a production seed if those
// credentials differ, including when a database contains development defaults.
func verifySeedPasswords(ctx context.Context, tx pgx.Tx, data seedData, passwords seedPasswords) error {
	for _, account := range []struct {
		id       uuid.UUID
		password string
	}{{data.owner, passwords.owner}, {data.manager, passwords.manager}, {data.viewer, passwords.viewer}} {
		var hash string
		if err := tx.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, account.id).Scan(&hash); err != nil {
			return errors.New("verify production seed credentials failed")
		}
		if !auth.VerifyPassword(account.password, hash) {
			return errors.New("refusing to seed: existing demo account credentials differ from the supplied production credentials")
		}
	}
	return nil
}
