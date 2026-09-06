package inventory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type inventoryFixture struct{ organizationID, productID, warehouseID uuid.UUID }

func newInventoryFixture(t *testing.T, pool *pgxpool.Pool, name string) inventoryFixture {
	t.Helper()
	fixture := inventoryFixture{organizationID: uuid.New(), productID: uuid.New(), warehouseID: uuid.New()}
	_, err := pool.Exec(context.Background(), `INSERT INTO organizations (id,name,slug) VALUES ($1,$2,$3)`, fixture.organizationID, name, name+"-"+fixture.organizationID.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(context.Background(), `INSERT INTO products (id,organization_id,sku,name) VALUES ($1,$2,$3,$4)`, fixture.productID, fixture.organizationID, "SKU-"+fixture.productID.String(), "Product")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(context.Background(), `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, fixture.warehouseID, fixture.organizationID, "WH-"+fixture.warehouseID.String(), "Warehouse")
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set; PostgreSQL integration test skipped")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestInventoryRepositoryTenantIsolationAndDuplicate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	first := newInventoryFixture(t, pool, "Inventory A")
	second := newInventoryFixture(t, pool, "Inventory B")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id IN ($1,$2)`, first.organizationID, second.organizationID)
	})
	repository := NewRepository(pool)
	created, err := repository.Create(ctx, first.organizationID, CreateInput{ProductID: first.productID, WarehouseID: first.warehouseID})
	if err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(ctx, first.organizationID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.ProductID != first.productID || got.WarehouseID != first.warehouseID || got.OnHandQuantity != 0 {
		t.Fatalf("get inventory = %+v, want id=%s product=%s warehouse=%s quantity=0", got, created.ID, first.productID, first.warehouseID)
	}
	if _, err := repository.Create(ctx, first.organizationID, CreateInput{ProductID: first.productID, WarehouseID: first.warehouseID}); !errors.Is(err, ErrInventoryExists) {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := repository.Create(ctx, first.organizationID, CreateInput{ProductID: second.productID, WarehouseID: first.warehouseID}); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("foreign product error = %v", err)
	}
	if _, err := repository.Create(ctx, first.organizationID, CreateInput{ProductID: first.productID, WarehouseID: second.warehouseID}); !errors.Is(err, ErrWarehouseNotFound) {
		t.Fatalf("foreign warehouse error = %v", err)
	}
	if _, err := repository.Get(ctx, second.organizationID, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get error = %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,-1)`, first.organizationID, first.productID, first.warehouseID); err == nil {
		t.Fatal("negative inventory insert succeeded")
	}
}

func TestInventoryRepositoryConcurrentDecrements(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fixture := newInventoryFixture(t, pool, "Inventory Decrement")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, fixture.organizationID) })
	repository := NewRepository(pool)
	item, err := repository.Create(ctx, fixture.organizationID, CreateInput{ProductID: fixture.productID, WarehouseID: fixture.warehouseID, OnHandQuantity: 5})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for operation := 1; operation <= 2; operation++ {
		go func(operation int) {
			defer wait.Done()
			_, operationErr := repository.Adjust(ctx, fixture.organizationID, item.ID, -4)
			results <- operationErr
			t.Logf("operation %d result: %v", operation, operationErr)
		}(operation)
	}
	wait.Wait()
	close(results)
	var success, insufficient int
	for err := range results {
		if err == nil {
			success++
		}
		if errors.Is(err, ErrInsufficientStock) {
			insufficient++
		}
	}
	final, err := repository.Get(ctx, fixture.organizationID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if success != 1 || insufficient != 1 || final.OnHandQuantity != 1 {
		t.Fatalf("success=%d insufficient=%d final=%d", success, insufficient, final.OnHandQuantity)
	}
}

func TestInventoryRepositoryConcurrentPositiveAdjustments(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fixture := newInventoryFixture(t, pool, "Inventory Positive")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, fixture.organizationID) })
	repository := NewRepository(pool)
	item, err := repository.Create(ctx, fixture.organizationID, CreateInput{ProductID: fixture.productID, WarehouseID: fixture.warehouseID})
	if err != nil {
		t.Fatal(err)
	}
	deltas := []int64{10, 15}
	results := make(chan error, len(deltas))
	var wait sync.WaitGroup
	wait.Add(len(deltas))
	for _, delta := range deltas {
		go func(delta int64) {
			defer wait.Done()
			_, operationErr := repository.Adjust(ctx, fixture.organizationID, item.ID, delta)
			results <- operationErr
			t.Logf("positive operation +%d result: %v", delta, operationErr)
		}(delta)
	}
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	final, err := repository.Get(ctx, fixture.organizationID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.OnHandQuantity != 25 {
		t.Fatalf("final positive quantity = %d, want 25", final.OnHandQuantity)
	}
}

func TestInventoryMigrationSchemaIsPresent(t *testing.T) {
	pool := testPool(t)
	var tableName string
	if err := pool.QueryRow(context.Background(), `SELECT table_name FROM information_schema.tables WHERE table_schema='public' AND table_name='inventory_levels'`).Scan(&tableName); err != nil {
		t.Fatal(fmt.Errorf("inventory migration is not applied: %w", err))
	}
}

func TestInventoryReservationAvailabilityAndLifecycle(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fixture := newInventoryFixture(t, pool, "Reservation Lifecycle")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, fixture.organizationID) })
	repository := NewRepository(pool)
	item, err := repository.Create(ctx, fixture.organizationID, CreateInput{ProductID: fixture.productID, WarehouseID: fixture.warehouseID, OnHandQuantity: 10})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := repository.Reserve(ctx, fixture.organizationID, item.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if reservation.Status != "active" || reservation.Quantity != 3 || !reservation.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("reservation = %+v", reservation)
	}
	got, err := repository.Get(ctx, fixture.organizationID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OnHandQuantity != 10 || got.ReservedQuantity != 3 || got.AvailableQuantity != 7 {
		t.Fatalf("get availability = %+v", got)
	}
	items, err := repository.List(ctx, fixture.organizationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].OnHandQuantity != 10 || items[0].ReservedQuantity != 3 || items[0].AvailableQuantity != 7 {
		t.Fatalf("list availability = %+v", items)
	}
	released, err := repository.Release(ctx, fixture.organizationID, reservation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if released.Status != "released" || released.ReleasedAt == nil {
		t.Fatalf("released reservation = %+v", released)
	}
	releasedAgain, err := repository.Release(ctx, fixture.organizationID, reservation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if releasedAgain.Status != "released" {
		t.Fatalf("released again = %+v", releasedAgain)
	}
	got, err = repository.Get(ctx, fixture.organizationID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReservedQuantity != 0 || got.AvailableQuantity != 10 {
		t.Fatalf("released availability = %+v", got)
	}
}

func TestInventoryReservationTenantIsolationAndConstraints(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	first := newInventoryFixture(t, pool, "Reservation Tenant A")
	second := newInventoryFixture(t, pool, "Reservation Tenant B")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id IN ($1,$2)`, first.organizationID, second.organizationID)
	})
	repository := NewRepository(pool)
	item, err := repository.Create(ctx, first.organizationID, CreateInput{ProductID: first.productID, WarehouseID: first.warehouseID, OnHandQuantity: 5})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := repository.Reserve(ctx, first.organizationID, item.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Reserve(ctx, second.organizationID, item.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign inventory reserve = %v", err)
	}
	if _, err := repository.Release(ctx, second.organizationID, reservation.ID); !errors.Is(err, ErrReservationNotFound) {
		t.Fatalf("foreign reservation release = %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_reservations (organization_id,inventory_level_id,quantity,expires_at) VALUES ($1,$2,1,NOW()+INTERVAL '15 minutes')`, second.organizationID, item.ID); err == nil {
		t.Fatal("cross-tenant reservation insert succeeded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_reservations (organization_id,inventory_level_id,quantity) VALUES ($1,$2,0)`, first.organizationID, item.ID); err == nil {
		t.Fatal("zero reservation insert succeeded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_reservations (organization_id,inventory_level_id,quantity,status,expires_at) VALUES ($1,$2,1,'unknown',NOW())`, first.organizationID, item.ID); err == nil {
		t.Fatal("invalid reservation status insert succeeded")
	}
}

func TestInventoryExpiredReservationDoesNotConsumeAvailability(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fixture := newInventoryFixture(t, pool, "Reservation Expiry")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, fixture.organizationID) })
	repository := NewRepository(pool)
	item, err := repository.Create(ctx, fixture.organizationID, CreateInput{ProductID: fixture.productID, WarehouseID: fixture.warehouseID, OnHandQuantity: 10})
	if err != nil {
		t.Fatal(err)
	}
	var expiredID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO inventory_reservations (organization_id,inventory_level_id,quantity,expires_at) VALUES ($1,$2,4,NOW()-INTERVAL '1 minute') RETURNING id`, fixture.organizationID, item.ID).Scan(&expiredID); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(ctx, fixture.organizationID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReservedQuantity != 0 || got.AvailableQuantity != 10 {
		t.Fatalf("expired availability = %+v", got)
	}
	if _, err := repository.Reserve(ctx, fixture.organizationID, item.ID, 6); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE id=$1`, expiredID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "expired" {
		t.Fatalf("lazy expiry status = %s", status)
	}
}

func TestInventoryReservationInactiveResources(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fixture := newInventoryFixture(t, pool, "Reservation Inactive")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, fixture.organizationID) })
	repository := NewRepository(pool)
	item, err := repository.Create(ctx, fixture.organizationID, CreateInput{ProductID: fixture.productID, WarehouseID: fixture.warehouseID, OnHandQuantity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET is_active=false WHERE id=$1`, fixture.productID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Reserve(ctx, fixture.organizationID, item.ID, 1); !errors.Is(err, ErrInactiveProduct) {
		t.Fatalf("inactive product reserve = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET is_active=true WHERE id=$1`, fixture.productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE warehouses SET is_active=false WHERE id=$1`, fixture.warehouseID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Reserve(ctx, fixture.organizationID, item.ID, 1); !errors.Is(err, ErrInactiveWarehouse) {
		t.Fatalf("inactive warehouse reserve = %v", err)
	}
}

func TestInventoryStockCannotFallBelowReserved(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fixture := newInventoryFixture(t, pool, "Reservation Stock Protection")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, fixture.organizationID) })
	repository := NewRepository(pool)
	item, err := repository.Create(ctx, fixture.organizationID, CreateInput{ProductID: fixture.productID, WarehouseID: fixture.warehouseID, OnHandQuantity: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Reserve(ctx, fixture.organizationID, item.ID, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Adjust(ctx, fixture.organizationID, item.ID, -8); !errors.Is(err, ErrStockBelowReserved) {
		t.Fatalf("below reserved error = %v", err)
	}
	if _, err := repository.Adjust(ctx, fixture.organizationID, item.ID, -7); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(ctx, fixture.organizationID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OnHandQuantity != 3 || got.ReservedQuantity != 3 || got.AvailableQuantity != 0 {
		t.Fatalf("protected stock = %+v", got)
	}
}

func TestInventoryConcurrentReservations(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fixture := newInventoryFixture(t, pool, "Reservation Concurrent")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, fixture.organizationID) })
	repository := NewRepository(pool)
	item, err := repository.Create(ctx, fixture.organizationID, CreateInput{ProductID: fixture.productID, WarehouseID: fixture.warehouseID, OnHandQuantity: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Reserve(ctx, fixture.organizationID, item.ID, 9); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for operation := 1; operation <= 2; operation++ {
		go func(operation int) {
			defer wait.Done()
			_, operationErr := repository.Reserve(ctx, fixture.organizationID, item.ID, 1)
			results <- operationErr
			t.Logf("reservation operation %d result: %v", operation, operationErr)
		}(operation)
	}
	wait.Wait()
	close(results)
	success, insufficient := 0, 0
	for err := range results {
		if err == nil {
			success++
		}
		if errors.Is(err, ErrInsufficientAvailableStock) {
			insufficient++
		}
	}
	final, err := repository.Get(ctx, fixture.organizationID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reservation success=%d insufficient=%d final reserved=%d available=%d", success, insufficient, final.ReservedQuantity, final.AvailableQuantity)
	if success != 1 || insufficient != 1 || final.OnHandQuantity != 10 || final.ReservedQuantity != 10 || final.AvailableQuantity != 0 {
		t.Fatalf("concurrent reservation result success=%d insufficient=%d final=%+v", success, insufficient, final)
	}
}

func TestInventoryConcurrentReleaseAndReserve(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fixture := newInventoryFixture(t, pool, "Reservation Release Race")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, fixture.organizationID) })
	repository := NewRepository(pool)
	item, err := repository.Create(ctx, fixture.organizationID, CreateInput{ProductID: fixture.productID, WarehouseID: fixture.warehouseID, OnHandQuantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := repository.Reserve(ctx, fixture.organizationID, item.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, releaseErr := repository.Release(ctx, fixture.organizationID, reservation.ID)
		results <- releaseErr
	}()
	go func() {
		defer wait.Done()
		_, reserveErr := repository.Reserve(ctx, fixture.organizationID, item.ID, 1)
		results <- reserveErr
	}()
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil && !errors.Is(err, ErrInsufficientAvailableStock) {
			t.Fatal(err)
		}
	}
	final, err := repository.Get(ctx, fixture.organizationID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.ReservedQuantity < 0 || final.ReservedQuantity > final.OnHandQuantity || final.AvailableQuantity < 0 {
		t.Fatalf("release/reserve invariant violated: %+v", final)
	}
}
