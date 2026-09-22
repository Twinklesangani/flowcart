package auth

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func familyPool(t *testing.T, tracer pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func familyUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test')`, id, id.String()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, id); err != nil {
			t.Error(err)
		}
	})
	return id
}

func familySession(user uuid.UUID) Session {
	return Session{ID: uuid.New(), UserID: user, RefreshTokenHash: uuid.NewString(), ExpiresAt: time.Now().Add(time.Hour)}
}

func TestSessionFamiliesCreationIsolationAndDescendants(t *testing.T) {
	pool := familyPool(t, nil)
	repo := NewRepository(pool)
	user := familyUser(t, pool)
	otherUser := familyUser(t, pool)
	ctx := context.Background()
	a, other, foreign := familySession(user), familySession(user), familySession(otherUser)
	for _, s := range []Session{a, other, foreign} {
		if err := repo.CreateSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	a, _ = repo.FindActiveSession(ctx, a.RefreshTokenHash)
	other, _ = repo.FindActiveSession(ctx, other.RefreshTokenHash)
	if a.FamilyID == uuid.Nil || a.FamilyID == other.FamilyID {
		t.Fatal("logins shared a family")
	}
	b, c := familySession(user), familySession(user)
	if err := repo.RotateSession(ctx, a.ID, b); err != nil {
		t.Fatal(err)
	}
	b, _ = repo.FindActiveSession(ctx, b.RefreshTokenHash)
	if b.FamilyID != a.FamilyID {
		t.Fatal("rotation changed family")
	}
	if err := repo.RotateSession(ctx, b.ID, c); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repo.RevokeSession(ctx, a.RefreshTokenHash); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []Session{a, b, c} {
		if _, err := repo.FindActiveSession(ctx, s.RefreshTokenHash); !errors.Is(err, ErrNotFound) {
			t.Fatalf("descendant accepted: %v", err)
		}
		if err := repo.RotateSession(ctx, s.ID, familySession(user)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("revoked rotation accepted: %v", err)
		}
	}
	for _, s := range []Session{other, foreign} {
		if _, err := repo.FindActiveSession(ctx, s.RefreshTokenHash); err != nil {
			t.Fatalf("unrelated family revoked: %v", err)
		}
	}
	// A valid-looking row cannot override a family's revocation flag.
	if _, err := pool.Exec(ctx, `UPDATE auth_sessions SET revoked_at=NULL WHERE id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindActiveSession(ctx, c.RefreshTokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("family revocation ignored: %v", err)
	}
	if err := repo.RotateSession(ctx, c.ID, familySession(user)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked family rotated: %v", err)
	}
	if err := repo.RevokeSession(ctx, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
}

func TestSessionFamilyCreationAndRotationRollback(t *testing.T) {
	pool := familyPool(t, nil)
	repo := NewRepository(pool)
	user := familyUser(t, pool)
	ctx := context.Background()
	a := familySession(user)
	if err := repo.CreateSession(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateSession(ctx, a); err == nil {
		t.Fatal("duplicate session accepted")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth_session_families WHERE user_id=$1`, user).Scan(&count); err != nil || count != 1 {
		t.Fatalf("orphan family: %d %v", count, err)
	}
	replacement := familySession(user)
	replacement.RefreshTokenHash = a.RefreshTokenHash
	if err := repo.RotateSession(ctx, a.ID, replacement); err == nil {
		t.Fatal("duplicate token accepted")
	}
	if _, err := repo.FindActiveSession(ctx, a.RefreshTokenHash); err != nil {
		t.Fatalf("failed rotation revoked original: %v", err)
	}
}

// Pause the winner after it has acquired the family lock. The contender signals
// when it reaches its own family-lock query. No scheduling sleeps are used.
type familyLockGate struct {
	acquired, entered, release chan struct{}
	once                       sync.Once
}
type familyQueryKey struct{}

func (g *familyLockGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	lock := strings.Contains(data.SQL, "auth_session_families") && strings.Contains(data.SQL, "FOR UPDATE")
	if lock && g.entered != nil {
		g.once.Do(func() { close(g.entered) })
	}
	return context.WithValue(ctx, familyQueryKey{}, lock)
}
func (g *familyLockGate) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(familyQueryKey{}) == true && data.Err == nil && g.acquired != nil {
		g.once.Do(func() {
			close(g.acquired)
			select {
			case <-g.release:
			case <-ctx.Done():
			}
		})
	}
}

func TestSessionFamilyConcurrentOrderings(t *testing.T) {
	for _, ordering := range []string{"logout-first", "rotation-first", "two-rotations"} {
		t.Run(ordering, func(t *testing.T) {
			pool := familyPool(t, nil)
			user := familyUser(t, pool)
			a, b, c := familySession(user), familySession(user), familySession(user)
			if err := NewRepository(pool).CreateSession(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			winnerGate := &familyLockGate{acquired: make(chan struct{}), release: make(chan struct{})}
			contenderGate := &familyLockGate{entered: make(chan struct{})}
			winner := NewRepository(familyPool(t, winnerGate))
			contender := NewRepository(familyPool(t, contenderGate))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			first, second := make(chan error, 1), make(chan error, 1)
			go func() {
				if ordering == "logout-first" {
					first <- winner.RevokeSession(ctx, a.RefreshTokenHash)
				} else {
					first <- winner.RotateSession(ctx, a.ID, b)
				}
			}()
			select {
			case <-winnerGate.acquired:
			case <-ctx.Done():
				t.Fatal("winner never locked family")
			}
			go func() {
				if ordering == "rotation-first" {
					second <- contender.RevokeSession(ctx, a.RefreshTokenHash)
				} else {
					second <- contender.RotateSession(ctx, a.ID, c)
				}
			}()
			select {
			case <-contenderGate.entered:
			case <-ctx.Done():
				t.Fatal("contender never attempted lock")
			}
			close(winnerGate.release)
			if err := <-first; err != nil {
				t.Fatal(err)
			}
			err := <-second
			if ordering == "rotation-first" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrNotFound) {
				t.Fatalf("second rotation: %v", err)
			}
			var active, total int
			if err := pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE revoked_at IS NULL),count(*) FROM auth_sessions WHERE user_id=$1`, user).Scan(&active, &total); err != nil {
				t.Fatal(err)
			}
			wantActive, wantTotal := 0, 2
			if ordering == "logout-first" {
				wantTotal = 1
			}
			if ordering == "two-rotations" {
				wantActive = 1
			}
			if active != wantActive || total != wantTotal {
				t.Fatalf("active=%d total=%d", active, total)
			}
		})
	}
}

func TestSessionFamilyMigrationBackfillAndRoundTrip(t *testing.T) {
	pool := familyPool(t, nil)
	ctx := context.Background()
	for _, withRows := range []bool{false, true} {
		schema := "auth_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer conn.Release()
			if _, err := conn.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_, _ = conn.Exec(ctx, `SET search_path TO public`)
				_, _ = conn.Exec(ctx, `DROP SCHEMA `+schema+` CASCADE`)
			}()
			if _, err := conn.Exec(ctx, `SET search_path TO `+schema+`,public`); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"000001_core_saas_tables.up.sql", "000002_authentication.up.sql"} {
				body, err := os.ReadFile("../../migrations/" + name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := conn.Exec(ctx, string(body)); err != nil {
					t.Fatal(err)
				}
			}
			if withRows {
				if _, err := conn.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES(gen_random_uuid(),'migration@example.test','test'); INSERT INTO auth_sessions(user_id,refresh_token_hash,expires_at) SELECT id,gen_random_uuid()::text,NOW()+INTERVAL '1 day' FROM users; INSERT INTO auth_sessions(user_id,refresh_token_hash,expires_at,revoked_at) SELECT id,gen_random_uuid()::text,NOW()-INTERVAL '1 day',NOW()-INTERVAL '1 hour' FROM users`); err != nil {
					t.Fatal(err)
				}
			}
			for _, direction := range []string{"up", "down", "up"} {
				body, err := os.ReadFile("../../migrations/000015_auth_session_families." + direction + ".sql")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := conn.Exec(ctx, string(body)); err != nil {
					t.Fatal(err)
				}
				if direction == "up" {
					var invalid, families, sessions int
					if err := conn.QueryRow(ctx, `SELECT count(*) FROM auth_sessions s LEFT JOIN auth_session_families f ON f.id=s.family_id WHERE s.family_id IS NULL OR f.id IS NULL OR s.revoked_at IS NULL OR f.revoked_at IS NULL`).Scan(&invalid); err != nil || invalid != 0 {
						t.Fatalf("invalid backfill=%d: %v", invalid, err)
					}
					if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM auth_session_families),(SELECT count(*) FROM auth_sessions)`).Scan(&families, &sessions); err != nil || families != sessions {
						t.Fatalf("families=%d sessions=%d: %v", families, sessions, err)
					}
				}
			}
		}()
	}
}
