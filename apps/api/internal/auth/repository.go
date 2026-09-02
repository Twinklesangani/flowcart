package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrDuplicateEmail = errors.New("duplicate email")
var ErrNotFound = errors.New("not found")

// Repository contains only PostgreSQL operations used by authentication.
type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (repository *Repository) CreateUser(ctx context.Context, user User) (User, error) {
	var created User
	err := repository.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, $2, $3, $4)
		RETURNING id, email, password_hash, COALESCE(first_name, ''), COALESCE(last_name, '')`,
		user.Email, user.PasswordHash, user.FirstName, user.LastName,
	).Scan(&created.ID, &created.Email, &created.PasswordHash, &created.FirstName, &created.LastName)
	if err != nil {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return User{}, ErrDuplicateEmail
		}
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return created, nil
}

func (repository *Repository) FindUserByEmail(ctx context.Context, email string) (User, error) {
	return repository.findUser(ctx, `SELECT id, email, password_hash, COALESCE(first_name, ''), COALESCE(last_name, '') FROM users WHERE LOWER(email) = LOWER($1)`, email)
}

func (repository *Repository) FindUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	return repository.findUser(ctx, `SELECT id, email, password_hash, COALESCE(first_name, ''), COALESCE(last_name, '') FROM users WHERE id = $1`, id)
}

func (repository *Repository) findUser(ctx context.Context, query string, argument any) (User, error) {
	var user User
	err := repository.pool.QueryRow(ctx, query, argument).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.FirstName, &user.LastName)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	return user, nil
}

func (repository *Repository) CreateSession(ctx context.Context, session Session) error {
	_, err := repository.pool.Exec(ctx, `INSERT INTO auth_sessions (id, user_id, refresh_token_hash, expires_at) VALUES ($1, $2, $3, $4)`, session.ID, session.UserID, session.RefreshTokenHash, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create auth session: %w", err)
	}
	return nil
}

func (repository *Repository) FindActiveSession(ctx context.Context, tokenHash string) (Session, error) {
	var session Session
	err := repository.pool.QueryRow(ctx, `SELECT id, user_id, refresh_token_hash, expires_at, revoked_at FROM auth_sessions WHERE refresh_token_hash = $1 AND revoked_at IS NULL AND expires_at > NOW()`, tokenHash).Scan(&session.ID, &session.UserID, &session.RefreshTokenHash, &session.ExpiresAt, &session.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("find auth session: %w", err)
	}
	return session, nil
}

func (repository *Repository) RotateSession(ctx context.Context, oldSessionID uuid.UUID, replacement Session) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin session rotation: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at = NOW(), last_used_at = NOW() WHERE id = $1 AND revoked_at IS NULL AND expires_at > NOW()`, oldSessionID)
	if err != nil {
		return fmt.Errorf("revoke old auth session: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_sessions (id, user_id, refresh_token_hash, expires_at) VALUES ($1, $2, $3, $4)`, replacement.ID, replacement.UserID, replacement.RefreshTokenHash, replacement.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create replacement auth session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit session rotation: %w", err)
	}
	return nil
}

func (repository *Repository) RevokeSession(ctx context.Context, tokenHash string) error {
	_, err := repository.pool.Exec(ctx, `UPDATE auth_sessions SET revoked_at = COALESCE(revoked_at, NOW()), last_used_at = NOW() WHERE refresh_token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("revoke auth session: %w", err)
	}
	return nil
}

func sessionExpiry(now time.Time, ttl time.Duration) time.Time { return now.Add(ttl) }
