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
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin auth session: %w", err)
	}
	defer tx.Rollback(ctx)
	familyID := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO auth_session_families (id,user_id) VALUES ($1,$2)`, familyID, session.UserID); err != nil {
		return fmt.Errorf("create session family: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_sessions (id, user_id, refresh_token_hash, expires_at, family_id) VALUES ($1, $2, $3, $4, $5)`, session.ID, session.UserID, session.RefreshTokenHash, session.ExpiresAt, familyID)
	if err != nil {
		return fmt.Errorf("create auth session: %w", err)
	}
	return tx.Commit(ctx)
}

func (repository *Repository) FindActiveSession(ctx context.Context, tokenHash string) (Session, error) {
	var session Session
	err := repository.pool.QueryRow(ctx, `SELECT s.id, s.user_id, s.refresh_token_hash, s.expires_at, s.revoked_at, s.family_id FROM auth_sessions s JOIN auth_session_families f ON f.id=s.family_id AND f.user_id=s.user_id WHERE s.refresh_token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > statement_timestamp() AND f.revoked_at IS NULL`, tokenHash).Scan(&session.ID, &session.UserID, &session.RefreshTokenHash, &session.ExpiresAt, &session.RevokedAt, &session.FamilyID)
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

	// Initial lookup takes no row lock. Every writer locks family before session.
	var familyID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT family_id FROM auth_sessions WHERE id=$1`, oldSessionID).Scan(&familyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("locate session family: %w", err)
	}
	var revokedAt *time.Time
	var userID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT revoked_at,user_id FROM auth_session_families WHERE id=$1 FOR UPDATE`, familyID).Scan(&revokedAt, &userID); err != nil {
		return fmt.Errorf("lock session family: %w", err)
	}
	if revokedAt != nil || userID != replacement.UserID {
		return ErrNotFound
	}
	// This conditional UPDATE locks and rechecks the session after the family
	// lock, using a fresh statement timestamp even after a lock wait.
	result, err := tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at = NOW(), last_used_at = NOW() WHERE id = $1 AND family_id=$2 AND user_id=$3 AND revoked_at IS NULL AND expires_at > statement_timestamp()`, oldSessionID, familyID, userID)
	if err != nil {
		return fmt.Errorf("revoke old auth session: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_sessions (id, user_id, refresh_token_hash, expires_at, family_id) VALUES ($1, $2, $3, $4, $5)`, replacement.ID, userID, replacement.RefreshTokenHash, replacement.ExpiresAt, familyID)
	if err != nil {
		return fmt.Errorf("create replacement auth session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit session rotation: %w", err)
	}
	return nil
}

func (repository *Repository) RevokeSession(ctx context.Context, tokenHash string) error {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin family logout: %w", err)
	}
	defer tx.Rollback(ctx)
	var familyID uuid.UUID
	// Rotated and expired predecessors must still identify their family.
	err = tx.QueryRow(ctx, `SELECT family_id FROM auth_sessions WHERE refresh_token_hash=$1`, tokenHash).Scan(&familyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("locate logout family: %w", err)
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM auth_session_families WHERE id=$1 FOR UPDATE`, familyID); err != nil {
		return fmt.Errorf("lock logout family: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE auth_session_families SET revoked_at=COALESCE(revoked_at,NOW()) WHERE id=$1`, familyID); err != nil {
		return fmt.Errorf("revoke session family: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE auth_sessions SET revoked_at = NOW(), last_used_at = NOW() WHERE family_id=$1 AND revoked_at IS NULL`, familyID)
	if err != nil {
		return fmt.Errorf("revoke auth session: %w", err)
	}
	return tx.Commit(ctx)
}

func sessionExpiry(now time.Time, ttl time.Duration) time.Time { return now.Add(ttl) }
