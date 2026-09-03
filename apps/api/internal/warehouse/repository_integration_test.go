package warehouse

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWarehouseRepositoryTenantIsolationAndUniqueCode(t *testing.T) {
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
	organizationA, organizationB := uuid.New(), uuid.New()
	warehouseID := uuid.New()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id IN ($1,$2)`, organizationA, organizationB)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO organizations (id,name,slug) VALUES ($1,$2,$3),($4,$5,$6)`, organizationA, "Warehouse Test A", "warehouse-test-"+organizationA.String(), organizationB, "Warehouse Test B", "warehouse-test-"+organizationB.String())
	if err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(pool)
	created, err := repository.Create(ctx, organizationA, CreateInput{Code: "MEL-01", Name: "Warehouse A"})
	if err != nil {
		t.Fatal(err)
	}
	warehouseID = created.ID
	if _, err := repository.Create(ctx, organizationB, CreateInput{Code: "MEL-01", Name: "Warehouse B"}); err != nil {
		t.Fatalf("same code in another tenant: %v", err)
	}
	if _, err := repository.Create(ctx, organizationA, CreateInput{Code: "mel-01", Name: "Duplicate"}); !errors.Is(err, ErrCodeTaken) {
		t.Fatalf("duplicate warehouse code error = %v", err)
	}
	if _, err := repository.Get(ctx, organizationB, warehouseID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get error = %v", err)
	}
	if _, err := repository.Update(ctx, organizationB, warehouseID, PatchInput{Name: stringPointer("Should Not Update")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant update error = %v", err)
	}
}
func stringPointer(value string) *string { return &value }
