package organization

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryConcurrentOwnerDemotionRetainsOwner(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set; PostgreSQL integration test skipped")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	userA, userB, organizationID := uuid.New(), uuid.New(), uuid.New()
	emailA := "milestone5-owner-a-" + userA.String() + "@example.test"
	emailB := "milestone5-owner-b-" + userB.String() + "@example.test"
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash) VALUES
		($1, $2, 'integration-test-password-hash'),
		($3, $4, 'integration-test-password-hash')`, userA, emailA, userB, emailB)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3)`, organizationID, "Milestone 5 Concurrent Owners", "milestone-5-"+organizationID.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, userA, userB)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO organization_members (organization_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'owner')`, organizationID, userA, userB)
	if err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(pool)
	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for _, userID := range []uuid.UUID{userA, userB} {
		waitGroup.Add(1)
		go func(userID uuid.UUID) {
			defer waitGroup.Done()
			<-start
			results <- repository.ChangeRole(ctx, organizationID, userID, RoleAdmin)
		}(userID)
	}
	close(start)
	waitGroup.Wait()
	close(results)

	var successful, lastOwnerErrors int
	for result := range results {
		switch {
		case result == nil:
			successful++
		case errors.Is(result, ErrLastOwner):
			lastOwnerErrors++
		default:
			t.Fatalf("unexpected concurrent demotion error: %v", result)
		}
	}
	if successful != 1 || lastOwnerErrors != 1 {
		t.Fatalf("concurrent demotion results: successful=%d last-owner=%d", successful, lastOwnerErrors)
	}
	t.Logf("concurrent demotion results: operation A/B produced one success and one ErrLastOwner")

	var ownerCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM organization_members WHERE organization_id=$1 AND role='owner'`, organizationID).Scan(&ownerCount)
	if err != nil {
		t.Fatal(err)
	}
	if ownerCount != 1 {
		t.Fatalf("final owner count = %d, want 1", ownerCount)
	}
	t.Logf("final PostgreSQL owner count = %d", ownerCount)

	var membershipCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM organization_members WHERE organization_id=$1`, organizationID).Scan(&membershipCount)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("organization membership query returned no rows")
	}
	if err != nil {
		t.Fatal(err)
	}
	if membershipCount != 2 {
		t.Fatalf("final membership count = %d, want 2", membershipCount)
	}
}
