package product

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProductRepositoryTenantIsolationAndUniqueSKU(t *testing.T) {
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
	productID := uuid.New()
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id IN ($1,$2)`, organizationA, organizationB)
	}()
	_, err = pool.Exec(ctx, `INSERT INTO organizations (id,name,slug) VALUES ($1,$2,$3),($4,$5,$6)`, organizationA, "Product Test A", "product-test-"+organizationA.String(), organizationB, "Product Test B", "product-test-"+organizationB.String())
	if err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(pool)
	created, err := repository.Create(ctx, organizationA, CreateInput{SKU: "TEST-001", Name: "Product A"})
	if err != nil {
		t.Fatal(err)
	}
	productID = created.ID
	if _, err := repository.Create(ctx, organizationB, CreateInput{SKU: "TEST-001", Name: "Product B"}); err != nil {
		t.Fatalf("same SKU in another tenant: %v", err)
	}
	if _, err := repository.Create(ctx, organizationA, CreateInput{SKU: "test-001", Name: "Duplicate"}); !errors.Is(err, ErrSKUTaken) {
		t.Fatalf("duplicate SKU error = %v", err)
	}
	if _, err := repository.Get(ctx, organizationB, productID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get error = %v", err)
	}
	if _, err := repository.Update(ctx, organizationB, productID, PatchInput{Name: stringPointer("Should Not Update")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant update error = %v", err)
	}
}
func stringPointer(value string) *string { return &value }

func TestProductPricingConstraintsAndPersistence(t *testing.T) {
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
	organizationID := uuid.New()
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, organizationID) }()
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id,name,slug) VALUES ($1,$2,$3)`, organizationID, "Pricing Test", "pricing-test-"+organizationID.String()); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(pool)
	price := int64(2550)
	currency := "AUD"
	created, err := repository.Create(ctx, organizationID, CreateInput{SKU: "PRICE-001", Name: "Priced Product", UnitPriceMinor: &price, CurrencyCode: &currency})
	if err != nil {
		t.Fatal(err)
	}
	if created.UnitPriceMinor == nil || *created.UnitPriceMinor != 2550 || created.CurrencyCode == nil || *created.CurrencyCode != "AUD" {
		t.Fatalf("pricing = %+v", created)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO products (organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,'HALF-PRICE','Half',1000,NULL)`, organizationID); err == nil {
		t.Fatal("half-priced product insert succeeded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO products (organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,'NEG-PRICE','Negative',-1,'AUD')`, organizationID); err == nil {
		t.Fatal("negative price insert succeeded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO products (organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,'BAD-CURRENCY','Bad',1000,'gbp')`, organizationID); err == nil {
		t.Fatal("lower-level invalid currency shape insert succeeded")
	}
}
