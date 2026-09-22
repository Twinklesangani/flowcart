package order

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type orderFixture struct{ organizationID, userID, productID, warehouseID, inventoryID uuid.UUID }

func orderPool(t *testing.T) *pgxpool.Pool {
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
func newOrderFixture(t *testing.T, pool *pgxpool.Pool, name string, quantity int64) orderFixture {
	t.Helper()
	ctx := context.Background()
	f := orderFixture{organizationID: uuid.New(), userID: uuid.New(), productID: uuid.New(), warehouseID: uuid.New(), inventoryID: uuid.New()}
	t.Cleanup(func() {
		cleanupOrderFixture(t, pool, f.organizationID)
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, f.userID); err != nil {
			t.Errorf("clean up order test user: %v", err)
		}
	})
	_, err := pool.Exec(ctx, `INSERT INTO organizations (id,name,slug) VALUES ($1,$2,$3)`, f.organizationID, name, name+"-"+f.organizationID.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO users (id,email,password_hash) VALUES ($1,$2,'test-hash')`, f.userID, "order-"+f.userID.String()+"@example.com")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO organization_members (organization_id,user_id,role) VALUES ($1,$2,'owner')`, f.organizationID, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,$4,2000,'AUD')`, f.productID, f.organizationID, "ORD-"+f.productID.String(), "Order Product")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, f.warehouseID, f.organizationID, "ORD-"+f.warehouseID.String(), "Order Warehouse")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,$5)`, f.inventoryID, f.organizationID, f.productID, f.warehouseID, quantity)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func cleanupOrderFixture(t *testing.T, pool *pgxpool.Pool, organizationID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Errorf("begin order test cleanup: %v", err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `ALTER TABLE audit_events DISABLE TRIGGER audit_events_append_only_trg`); err != nil {
		t.Errorf("disable audit append-only trigger during order cleanup: %v", err)
		return
	}
	// Delete dependent ledger, fulfillment, and audit rows before their referenced parents.
	for _, query := range []string{
		`DELETE FROM audit_events WHERE organization_id=$1`,
		`DELETE FROM inventory_movements WHERE organization_id=$1`,
		`DELETE FROM fulfillment_items WHERE organization_id=$1`,
		`DELETE FROM payment_provider_events WHERE organization_id=$1`,
		`DELETE FROM fulfillments WHERE organization_id=$1`,
		`DELETE FROM payments WHERE organization_id=$1`,
		`DELETE FROM inventory_reservations WHERE organization_id=$1`,
		`DELETE FROM order_items WHERE organization_id=$1`,
		`DELETE FROM orders WHERE organization_id=$1`,
		`DELETE FROM inventory_levels WHERE organization_id=$1`,
		`DELETE FROM products WHERE organization_id=$1`,
		`DELETE FROM warehouses WHERE organization_id=$1`,
		`DELETE FROM organization_members WHERE organization_id=$1`,
		`DELETE FROM organizations WHERE id=$1`,
	} {
		if _, err := tx.Exec(ctx, query, organizationID); err != nil {
			t.Errorf("clean up order test organization: %v", err)
			return
		}
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE audit_events ENABLE TRIGGER audit_events_append_only_trg`); err != nil {
		t.Errorf("re-enable audit append-only trigger after order cleanup: %v", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		t.Errorf("commit order test cleanup: %v", err)
	}
}
func orderCreateInput(inventoryID uuid.UUID, quantity int64) CreateInput {
	return CreateInput{Items: []ItemInput{{InventoryLevelID: inventoryID, Quantity: quantity}}}
}

func TestOrderRepositoryAtomicCreateSnapshotsReplayAndCancel(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Order Lifecycle", 5)
	t.Cleanup(func() { cleanupOrderFixture(t, pool, f.organizationID) })
	repository := NewRepository(pool)
	created, err := repository.Create(ctx, f.organizationID, f.userID, "lifecycle-key", strings.Repeat("1", 64), orderCreateInput(f.inventoryID, 2))
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusPending || len(created.Items) != 1 || created.Items[0].ReservationID == nil || created.Items[0].Quantity != 2 {
		t.Fatalf("created order = %+v", created)
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET name='Renamed Product',is_active=false WHERE id=$1`, f.productID); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(ctx, f.organizationID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Items[0].ProductNameSnapshot != "Order Product" {
		t.Fatalf("snapshot changed = %q", got.Items[0].ProductNameSnapshot)
	}
	replay, err := repository.Create(ctx, f.organizationID, f.userID, "lifecycle-key", strings.Repeat("1", 64), orderCreateInput(f.inventoryID, 2))
	if err != nil || replay.ID != created.ID {
		t.Fatalf("replay = %v %+v", err, replay)
	}
	if _, err := repository.Create(ctx, f.organizationID, f.userID, "lifecycle-key", strings.Repeat("2", 64), orderCreateInput(f.inventoryID, 1)); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("different replay error = %v", err)
	}
	cancelled, err := repository.Cancel(ctx, f.organizationID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != StatusCancelled {
		t.Fatalf("cancelled = %+v", cancelled)
	}
	cancelledAgain, err := repository.Cancel(ctx, f.organizationID, created.ID)
	if err != nil || cancelledAgain.Status != StatusCancelled {
		t.Fatalf("cancel again = %v %+v", err, cancelledAgain)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE order_item_id=$1`, created.Items[0].ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "released" {
		t.Fatalf("reservation status = %s", status)
	}
	if _, err := repository.Cancel(ctx, f.organizationID, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing cancellation error = %v", err)
	}
}

func TestAutomaticOrderSplitsProductIntoOneItemAndMultipleReservations(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Automatic Allocation", 5)
	secondWarehouseID, secondInventoryID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, secondWarehouseID, f.organizationID, "ORD-SECOND", "Second Warehouse"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,4)`, secondInventoryID, f.organizationID, f.productID, secondWarehouseID); err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(pool)
	created, err := repository.AutoCreate(ctx, f.organizationID, f.userID, "automatic-split", strings.Repeat("a", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 8}}})
	if err != nil {
		t.Fatal(err)
	}
	if created.AllocationMethod != "automatic" || created.AllocationStrategy == nil || *created.AllocationStrategy != "minimize_splits_v1" {
		t.Fatalf("allocation metadata = %+v", created)
	}
	if len(created.Items) != 1 || len(created.Items[0].Allocations) != 2 || len(created.Allocations) != 2 {
		t.Fatalf("expected one item and two allocations, got %+v", created)
	}
	var reservationCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE oi.order_id=$1`, created.ID).Scan(&reservationCount); err != nil {
		t.Fatal(err)
	}
	if reservationCount != 2 {
		t.Fatalf("reservation count = %d", reservationCount)
	}
	replayed, err := repository.AutoCreate(ctx, f.organizationID, f.userID, "automatic-split", strings.Repeat("a", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 8}}})
	if err != nil || replayed.ID != created.ID || len(replayed.Items) != 1 || len(replayed.Items[0].Allocations) != 2 {
		t.Fatalf("replay = %v %+v", err, replayed)
	}
}

func TestAutomaticOrderInsufficientNetworkStockRollsBack(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Automatic Rollback", 9)
	repository := NewRepository(pool)
	if _, err := repository.AutoCreate(ctx, f.organizationID, f.userID, "automatic-short", strings.Repeat("b", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 10}}}); !errors.Is(err, ErrInsufficientNetworkStock) {
		t.Fatalf("automatic shortage error = %v", err)
	}
	var orders, items, reservations int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE organization_id=$1`, f.organizationID).Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_items WHERE organization_id=$1`, f.organizationID).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE organization_id=$1`, f.organizationID).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if orders != 0 || items != 0 || reservations != 0 {
		t.Fatalf("automatic rollback left orders=%d items=%d reservations=%d", orders, items, reservations)
	}
}

func TestAutomaticOrderCandidateBudgetRejectsAndRollsBack(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Automatic Candidate Budget", 1)
	for index := 0; index < autoCandidateBudget; index++ {
		warehouseID, inventoryID := uuid.New(), uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, warehouseID, f.organizationID, "BUDGET-"+warehouseID.String(), "Budget Warehouse"); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,1)`, inventoryID, f.organizationID, f.productID, warehouseID); err != nil {
			t.Fatal(err)
		}
	}
	repository := NewRepository(pool)
	if _, err := repository.AutoCreate(ctx, f.organizationID, f.userID, "automatic-candidate-budget", strings.Repeat("c", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 1}}}); !errors.Is(err, ErrAutomaticAllocationComplexity) {
		t.Fatalf("candidate budget error = %v", err)
	}
	var orders, reservations int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE organization_id=$1`, f.organizationID).Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE organization_id=$1`, f.organizationID).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if orders != 0 || reservations != 0 {
		t.Fatalf("candidate budget rollback left orders=%d reservations=%d", orders, reservations)
	}
}

func TestAutomaticOrderReservationBudgetRejectsBeforeInserts(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Automatic Reservation Budget", 1)
	for index := 0; index < autoReservationBudget; index++ {
		warehouseID, inventoryID := uuid.New(), uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, warehouseID, f.organizationID, "RESERVE-"+warehouseID.String(), "Reservation Warehouse"); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,1)`, inventoryID, f.organizationID, f.productID, warehouseID); err != nil {
			t.Fatal(err)
		}
	}
	repository := NewRepository(pool)
	if _, err := repository.AutoCreate(ctx, f.organizationID, f.userID, "automatic-reservation-budget", strings.Repeat("r", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: autoReservationBudget + 1}}}); !errors.Is(err, ErrAutomaticAllocationComplexity) {
		t.Fatalf("reservation budget error = %v", err)
	}
	var orders, items, reservations int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE organization_id=$1`, f.organizationID).Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_items WHERE organization_id=$1`, f.organizationID).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE organization_id=$1`, f.organizationID).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if orders != 0 || items != 0 || reservations != 0 {
		t.Fatalf("reservation budget rollback left orders=%d items=%d reservations=%d", orders, items, reservations)
	}
}

func TestOrderRepositoryAtomicRollbackAndTenantIsolation(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Order Rollback", 1)
	other := newOrderFixture(t, pool, "Order Other", 1)
	t.Cleanup(func() {
		cleanupOrderFixture(t, pool, f.organizationID)
		cleanupOrderFixture(t, pool, other.organizationID)
	})
	repository := NewRepository(pool)
	if _, err := repository.Create(ctx, f.organizationID, f.userID, "rollback-key", strings.Repeat("3", 64), orderCreateInput(f.inventoryID, 2)); !errors.Is(err, ErrInsufficientAvailableStock) {
		t.Fatalf("rollback error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE organization_id=$1`, f.organizationID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("orders after rollback = %d", count)
	}
	if _, err := repository.Get(ctx, other.organizationID, f.inventoryID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign order inventory error = %v", err)
	}
}

func TestOrderRepositoryConcurrentFinalUnit(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Order Concurrent", 1)
	t.Cleanup(func() { cleanupOrderFixture(t, pool, f.organizationID) })
	repository := NewRepository(pool)
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wait.Done()
			_, err := repository.Create(ctx, f.organizationID, f.userID, "concurrent-"+uuid.New().String(), strings.Repeat("4", 64), orderCreateInput(f.inventoryID, 1))
			results <- err
		}(i)
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
	if success != 1 || insufficient != 1 {
		t.Fatalf("success=%d insufficient=%d", success, insufficient)
	}
}

func TestOrderRepositoryConcurrentIdempotency(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Order Idempotency", 3)
	t.Cleanup(func() { cleanupOrderFixture(t, pool, f.organizationID) })
	repository := NewRepository(pool)
	results := make(chan Order, 2)
	errorsOut := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wait.Done()
			item, err := repository.Create(ctx, f.organizationID, f.userID, "same-key", strings.Repeat("5", 64), orderCreateInput(f.inventoryID, 1))
			results <- item
			errorsOut <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errorsOut)
	var id uuid.UUID
	for item := range results {
		if item.ID != uuid.Nil {
			if id == uuid.Nil {
				id = item.ID
			} else if id != item.ID {
				t.Fatalf("different replay IDs: %s and %s", id, item.ID)
			}
		}
	}
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE organization_id=$1`, f.organizationID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("order count = %d", count)
	}
}

func TestOrderRepositoryMultiInventoryOppositeOrderCompletes(t *testing.T) {
	pool := orderPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f := newOrderFixture(t, pool, "Order Deadlock", 2)
	t.Cleanup(func() { cleanupOrderFixture(t, pool, f.organizationID) })
	secondProduct, secondWarehouse, secondInventory := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,$4,2000,'AUD')`, secondProduct, f.organizationID, "ORD-"+secondProduct.String(), "Second Product"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, secondWarehouse, f.organizationID, "ORD-"+secondWarehouse.String(), "Second Warehouse"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,2)`, secondInventory, f.organizationID, secondProduct, secondWarehouse); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(pool)
	results := make(chan error, 2)
	go func() {
		_, err := repository.Create(ctx, f.organizationID, f.userID, "deadlock-a", strings.Repeat("6", 64), CreateInput{Items: []ItemInput{{InventoryLevelID: f.inventoryID, Quantity: 1}, {InventoryLevelID: secondInventory, Quantity: 1}}})
		results <- err
	}()
	go func() {
		_, err := repository.Create(ctx, f.organizationID, f.userID, "deadlock-b", strings.Repeat("7", 64), CreateInput{Items: []ItemInput{{InventoryLevelID: secondInventory, Quantity: 1}, {InventoryLevelID: f.inventoryID, Quantity: 1}}})
		results <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil && !errors.Is(err, ErrInsufficientAvailableStock) {
			t.Fatal(err)
		}
	}
}

func TestOrderRepositoryPricingSnapshotsTotalsAndReplay(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Order Pricing", 10)
	t.Cleanup(func() { cleanupOrderFixture(t, pool, f.organizationID) })
	secondProduct, secondWarehouse, secondInventory := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,$4,4000,'AUD')`, secondProduct, f.organizationID, "PRICE-"+secondProduct.String(), "Second Price Product"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, secondWarehouse, f.organizationID, "PRICE-"+secondWarehouse.String(), "Second Warehouse"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,10)`, secondInventory, f.organizationID, secondProduct, secondWarehouse); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(pool)
	input := CreateInput{Items: []ItemInput{{InventoryLevelID: f.inventoryID, Quantity: 2}, {InventoryLevelID: secondInventory, Quantity: 1}}}
	created, err := repository.Create(ctx, f.organizationID, f.userID, "pricing-key", strings.Repeat("8", 64), input)
	if err != nil {
		t.Fatal(err)
	}
	if created.CurrencyCode == nil || *created.CurrencyCode != "AUD" || created.SubtotalMinor == nil || *created.SubtotalMinor != 8000 {
		t.Fatalf("order totals = %+v", created)
	}
	if len(created.Items) != 2 {
		t.Fatalf("items = %+v", created.Items)
	}
	for _, item := range created.Items {
		if item.UnitPriceMinorSnapshot == nil || item.CurrencyCodeSnapshot == nil || item.LineTotalMinor == nil {
			t.Fatalf("item pricing = %+v", item)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET unit_price_minor=2500 WHERE id=$1`, f.productID); err != nil {
		t.Fatal(err)
	}
	got, err := repository.Get(ctx, f.organizationID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var mainItem *OrderItem
	for index := range got.Items {
		if got.Items[index].ProductID == f.productID {
			mainItem = &got.Items[index]
		}
	}
	if got.SubtotalMinor == nil || *got.SubtotalMinor != 8000 || mainItem == nil || mainItem.UnitPriceMinorSnapshot == nil || *mainItem.UnitPriceMinorSnapshot != 2000 {
		t.Fatalf("historical pricing changed = %+v", got)
	}
	replay, err := repository.Create(ctx, f.organizationID, f.userID, "pricing-key", strings.Repeat("8", 64), input)
	if err != nil || replay.ID != created.ID || replay.SubtotalMinor == nil || *replay.SubtotalMinor != 8000 {
		t.Fatalf("pricing replay = %v %+v", err, replay)
	}
}

func TestOrderRepositoryMixedCurrencyAndZeroPrice(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Order Currency", 5)
	t.Cleanup(func() { cleanupOrderFixture(t, pool, f.organizationID) })
	foreignProduct, foreignWarehouse, foreignInventory := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,$4,1000,'USD')`, foreignProduct, f.organizationID, "USD-"+foreignProduct.String(), "USD Product"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, foreignWarehouse, f.organizationID, "USD-"+foreignWarehouse.String(), "USD Warehouse"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,5)`, foreignInventory, f.organizationID, foreignProduct, foreignWarehouse); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(pool)
	if _, err := repository.Create(ctx, f.organizationID, f.userID, "mixed-key", strings.Repeat("9", 64), CreateInput{Items: []ItemInput{{InventoryLevelID: f.inventoryID, Quantity: 1}, {InventoryLevelID: foreignInventory, Quantity: 1}}}); !errors.Is(err, ErrMixedCurrencyOrder) {
		t.Fatalf("mixed currency error = %v", err)
	}
	zeroProduct, zeroWarehouse, zeroInventory := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,$4,0,'AUD')`, zeroProduct, f.organizationID, "ZERO-"+zeroProduct.String(), "Zero Product"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, zeroWarehouse, f.organizationID, "ZERO-"+zeroWarehouse.String(), "Zero Warehouse"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,5)`, zeroInventory, f.organizationID, zeroProduct, zeroWarehouse); err != nil {
		t.Fatal(err)
	}
	zeroOrder, err := repository.Create(ctx, f.organizationID, f.userID, "zero-key", strings.Repeat("a", 64), orderCreateInput(zeroInventory, 2))
	if err != nil {
		t.Fatal(err)
	}
	if zeroOrder.SubtotalMinor == nil || *zeroOrder.SubtotalMinor != 0 {
		t.Fatalf("zero subtotal = %+v", zeroOrder)
	}
}

func TestOrderRepositoryPriceUpdateRace(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Order Price Race", 2)
	t.Cleanup(func() { cleanupOrderFixture(t, pool, f.organizationID) })
	repository := NewRepository(pool)
	results := make(chan error, 1)
	go func() {
		_, err := repository.Create(ctx, f.organizationID, f.userID, "race-price-key", strings.Repeat("b", 64), orderCreateInput(f.inventoryID, 1))
		results <- err
	}()
	if _, err := pool.Exec(ctx, `UPDATE products SET unit_price_minor=2500 WHERE id=$1`, f.productID); err != nil {
		t.Fatal(err)
	}
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	var price, line, subtotal int64
	if err := pool.QueryRow(ctx, `SELECT oi.unit_price_minor_snapshot,oi.line_total_minor,o.subtotal_minor FROM orders o JOIN order_items oi ON oi.organization_id=o.organization_id AND oi.order_id=o.id WHERE o.organization_id=$1 AND o.idempotency_key='race-price-key'`, f.organizationID).Scan(&price, &line, &subtotal); err != nil {
		t.Fatal(err)
	}
	if price != 2000 && price != 2500 {
		t.Fatalf("race price = %d", price)
	}
	if line != price || subtotal != line {
		t.Fatalf("inconsistent race totals price=%d line=%d subtotal=%d", price, line, subtotal)
	}
}

func TestOrderPricingSnapshotDatabaseConstraints(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Order Pricing Constraints", 1)
	t.Cleanup(func() { cleanupOrderFixture(t, pool, f.organizationID) })
	var orderID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO orders (organization_id,created_by_user_id,idempotency_key,request_hash) VALUES ($1,$2,'constraint-key',$3) RETURNING id`, f.organizationID, f.userID, strings.Repeat("c", 64)).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET currency_code='AUD' WHERE id=$1`, orderID); err == nil {
		t.Fatal("half-populated order totals update succeeded")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO order_items (organization_id,order_id,product_id,sku_snapshot,product_name_snapshot,quantity,unit_price_minor_snapshot,currency_code_snapshot,line_total_minor) VALUES ($1,$2,$3,'SKU','Name',1,2000,'AUD',NULL)`, f.organizationID, orderID, f.productID); err == nil {
		t.Fatal("partial order item pricing insert succeeded")
	}
}
