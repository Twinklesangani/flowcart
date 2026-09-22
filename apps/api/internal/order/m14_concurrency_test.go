package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"flowcart/apps/api/internal/inventory"
	"flowcart/apps/api/internal/warehouse"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func boolPtr(v bool) *bool { return &v }

func TestAutomaticOrderWarehouseDeactivationRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprintf("loop-%d", i), func(t *testing.T) {
			pool := orderPool(t)
			ctx := context.Background()
			f := newOrderFixture(t, pool, "Auto Warehouse Deactivation", 10)
			repository := NewRepository(pool)
			warehouseRepository := warehouse.NewRepository(pool)
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() {
				<-start
				_, err := repository.AutoCreate(ctx, f.organizationID, f.userID, fmt.Sprintf("warehouse-race-%d", i), strings.Repeat("w", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 7}}})
				results <- err
			}()
			go func() {
				<-start
				_, err := warehouseRepository.Update(ctx, f.organizationID, f.warehouseID, warehouse.PatchInput{IsActive: boolPtr(false)})
				results <- err
			}()
			close(start)
			var autoErr, warehouseErr error
			for j := 0; j < 2; j++ {
				err := <-results
				if j == 0 {
					autoErr = err
				} else {
					warehouseErr = err
				}
			}
			if warehouseErr != nil && !errors.Is(warehouseErr, ErrInsufficientNetworkStock) && !errors.Is(warehouseErr, inventory.ErrInactiveWarehouse) && !errors.Is(warehouseErr, ErrNotFound) {
				t.Fatalf("unexpected warehouse update error: %v", warehouseErr)
			}
			var warehouseInactive bool
			if err := pool.QueryRow(ctx, `SELECT is_active FROM warehouses WHERE organization_id=$1 AND id=$2`, f.organizationID, f.warehouseID).Scan(&warehouseInactive); err != nil {
				t.Fatal(err)
			}
			var reservationCreatedAt *time.Time
			if err := pool.QueryRow(ctx, `SELECT created_at FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2 ORDER BY created_at DESC LIMIT 1`, f.organizationID, f.inventoryID).Scan(&reservationCreatedAt); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				t.Fatal(err)
			}
			var warehouseUpdatedAt time.Time
			if err := pool.QueryRow(ctx, `SELECT updated_at FROM warehouses WHERE organization_id=$1 AND id=$2`, f.organizationID, f.warehouseID).Scan(&warehouseUpdatedAt); err != nil {
				t.Fatal(err)
			}
			if reservationCreatedAt != nil && warehouseInactive && reservationCreatedAt.After(warehouseUpdatedAt) {
				t.Fatalf("forbidden: warehouse deactivation committed first and auto order later created a reservation: autoErr=%v warehouseInactive=%v warehouseUpdatedAt=%s reservationCreatedAt=%s", autoErr, warehouseInactive, warehouseUpdatedAt, reservationCreatedAt)
			}
			if autoErr != nil && !errors.Is(autoErr, ErrInsufficientNetworkStock) && !errors.Is(autoErr, inventory.ErrInactiveWarehouse) && !errors.Is(autoErr, ErrNotFound) {
				t.Fatalf("unexpected automatic allocation error: %v", autoErr)
			}
			if warehouseErr == nil && warehouseInactive && autoErr == nil {
				var finalReserved int64
				if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(quantity),0) FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2 AND status IN ('active','payment_held','committed')`, f.organizationID, f.inventoryID).Scan(&finalReserved); err != nil {
					t.Fatal(err)
				}
				if finalReserved > 10 {
					t.Fatalf("final reserved stock=%d exceeded on-hand guard", finalReserved)
				}
			}
			cleanupOrderFixture(t, pool, f.organizationID)
		})
	}
}

func TestAutomaticOrderPriceUpdateRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprintf("price-race-%d", i), func(t *testing.T) {
			pool := orderPool(t)
			ctx := context.Background()
			f := newOrderFixture(t, pool, "Auto Price Race", 10)
			repository := NewRepository(pool)
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() {
				<-start
				_, err := repository.AutoCreate(ctx, f.organizationID, f.userID, fmt.Sprintf("price-race-%d", i), strings.Repeat("p", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 3}}})
				results <- err
			}()
			go func() {
				<-start
				_, err := pool.Exec(ctx, `UPDATE products SET unit_price_minor=2500,currency_code='USD' WHERE organization_id=$1 AND id=$2`, f.organizationID, f.productID)
				results <- err
			}()
			close(start)
			for j := 0; j < 2; j++ {
				if err := <-results; err != nil {
					if j == 0 {
						t.Logf("auto order error: %v", err)
					} else {
						t.Logf("price update error: %v", err)
					}
				}
			}
			var orderID uuid.UUID
			if err := pool.QueryRow(ctx, `SELECT id FROM orders WHERE organization_id=$1 AND idempotency_key=$2`, f.organizationID, fmt.Sprintf("price-race-%d", i)).Scan(&orderID); err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatal(err)
				}
				return
			}
			var price, line, subtotal int64
			if err := pool.QueryRow(ctx, `SELECT oi.unit_price_minor_snapshot, oi.line_total_minor, o.subtotal_minor FROM orders o JOIN order_items oi ON oi.organization_id=o.organization_id AND oi.order_id=o.id WHERE o.organization_id=$1 AND o.id=$2`, f.organizationID, orderID).Scan(&price, &line, &subtotal); err != nil {
				t.Fatal(err)
			}
			if price != 2000 && price != 2500 {
				t.Fatalf("unexpected price snapshot: %d", price)
			}
			if line != price*3 || subtotal != line {
				t.Fatalf("inconsistent pricing snapshot: price=%d quantity=3 line=%d subtotal=%d", price, line, subtotal)
			}
			cleanupOrderFixture(t, pool, f.organizationID)
		})
	}
}

func TestAutomaticOrderSameIdempotencyKeyRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprintf("same-key-%d", i), func(t *testing.T) {
			pool := orderPool(t)
			ctx := context.Background()
			f := newOrderFixture(t, pool, "Auto Same Key", 20)
			repository := NewRepository(pool)
			key := fmt.Sprintf("same-key-%d", i)
			results := make(chan Order, 2)
			errorsOut := make(chan error, 2)
			var wait sync.WaitGroup
			wait.Add(2)
			for j := 0; j < 2; j++ {
				go func() {
					defer wait.Done()
					item, err := repository.AutoCreate(ctx, f.organizationID, f.userID, key, strings.Repeat("k", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 3}}})
					results <- item
					errorsOut <- err
				}()
			}
			wait.Wait()
			close(results)
			close(errorsOut)
			var firstID uuid.UUID
			var successCount int
			for item := range results {
				if item.ID != uuid.Nil {
					successCount++
					if firstID == uuid.Nil {
						firstID = item.ID
					} else if firstID != item.ID {
						t.Fatalf("different order IDs returned: %s and %s", firstID, item.ID)
					}
				}
			}
			for err := range errorsOut {
				if err != nil && !errors.Is(err, ErrIdempotencyKeyReused) {
					t.Fatalf("unexpected same-key error: %v", err)
				}
			}
			if successCount == 0 {
				t.Fatalf("successCount=%d; want at least 1", successCount)
			}
			var orders int
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE organization_id=$1 AND idempotency_key=$2`, f.organizationID, key).Scan(&orders); err != nil {
				t.Fatal(err)
			}
			if orders != 1 {
				t.Fatalf("orders=%d want 1", orders)
			}
			var items int
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_items oi JOIN orders o ON o.organization_id=oi.organization_id AND o.id=oi.order_id WHERE o.organization_id=$1 AND o.idempotency_key=$2`, f.organizationID, key).Scan(&items); err != nil {
				t.Fatal(err)
			}
			if items != 1 {
				t.Fatalf("order_items=%d want 1", items)
			}
			var reservations int
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id JOIN orders o ON o.organization_id=oi.organization_id AND o.id=oi.order_id WHERE o.organization_id=$1 AND o.idempotency_key=$2`, f.organizationID, key).Scan(&reservations); err != nil {
				t.Fatal(err)
			}
			if reservations != 1 {
				t.Fatalf("reservations=%d want 1", reservations)
			}
			cleanupOrderFixture(t, pool, f.organizationID)
		})
	}
}

func TestAutomaticOrderScarceStockRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprintf("scarce-%d", i), func(t *testing.T) {
			pool := orderPool(t)
			ctx := context.Background()
			f := newOrderFixture(t, pool, "Auto Scarce", 10)
			repository := NewRepository(pool)
			results := make(chan error, 2)
			var wait sync.WaitGroup
			wait.Add(2)
			for j := 0; j < 2; j++ {
				go func(idx int) {
					defer wait.Done()
					_, err := repository.AutoCreate(ctx, f.organizationID, f.userID, fmt.Sprintf("scarce-%d-%d", i, idx), strings.Repeat("s", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 7}}})
					results <- err
				}(j)
			}
			wait.Wait()
			close(results)
			succeeded := 0
			for err := range results {
				if err == nil {
					succeeded++
				} else if !errors.Is(err, ErrInsufficientNetworkStock) {
					t.Fatalf("unexpected scarce-stock error: %v", err)
				}
			}
			if succeeded == 0 {
				t.Fatalf("succeeded=%d want at least 1", succeeded)
			}
			var reserved int64
			if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(r.quantity),0) FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE r.organization_id=$1 AND r.inventory_level_id=$2`, f.organizationID, f.inventoryID).Scan(&reserved); err != nil {
				t.Fatal(err)
			}
			if reserved > 10 {
				t.Fatalf("reserved=%d exceeds on_hand=10", reserved)
			}
			cleanupOrderFixture(t, pool, f.organizationID)
		})
	}
}

func TestAutomaticOrderMultiWarehouseScarceStockRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprintf("multiwarehouse-%d", i), func(t *testing.T) {
			pool := orderPool(t)
			ctx := context.Background()
			f := newOrderFixture(t, pool, "Auto Multi Scarce", 5)
			warehouseBID, inventoryBID := uuid.New(), uuid.New()
			if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, warehouseBID, f.organizationID, "MWS-B", "Warehouse B"); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,5)`, inventoryBID, f.organizationID, f.productID, warehouseBID); err != nil {
				t.Fatal(err)
			}
			repository := NewRepository(pool)
			results := make(chan error, 2)
			var wait sync.WaitGroup
			wait.Add(2)
			for j := 0; j < 2; j++ {
				go func(idx int) {
					defer wait.Done()
					_, err := repository.AutoCreate(ctx, f.organizationID, f.userID, fmt.Sprintf("multi-%d-%d", i, idx), strings.Repeat("m", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 6}}})
					results <- err
				}(j)
			}
			wait.Wait()
			close(results)
			succeeded := 0
			for err := range results {
				if err == nil {
					succeeded++
				} else if !errors.Is(err, ErrInsufficientNetworkStock) {
					t.Fatalf("unexpected multiwarehouse error: %v", err)
				}
			}
			if succeeded == 0 {
				t.Fatalf("succeeded=%d want at least 1", succeeded)
			}
			var totalReserved int64
			if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(r.quantity),0) FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE r.organization_id=$1 AND oi.product_id=$2`, f.organizationID, f.productID).Scan(&totalReserved); err != nil {
				t.Fatal(err)
			}
			if totalReserved > 10 {
				t.Fatalf("total_reserved=%d exceeds total on_hand=10", totalReserved)
			}
			cleanupOrderFixture(t, pool, f.organizationID)
		})
	}
}

func TestAutomaticOrderVsManualReservationRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprintf("manual-reserve-%d", i), func(t *testing.T) {
			pool := orderPool(t)
			ctx := context.Background()
			f := newOrderFixture(t, pool, "Auto Manual Reserve", 10)
			repository := NewRepository(pool)
			manualRepo := inventory.NewRepository(pool)
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() {
				<-start
				_, err := repository.AutoCreate(ctx, f.organizationID, f.userID, fmt.Sprintf("manual-reserve-auto-%d", i), strings.Repeat("r", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 7}}})
				results <- err
			}()
			go func() {
				<-start
				_, err := manualRepo.Reserve(ctx, f.organizationID, f.inventoryID, 4)
				results <- err
			}()
			close(start)
			for j := 0; j < 2; j++ {
				err := <-results
				if err != nil {
					if j == 0 && !errors.Is(err, ErrInsufficientNetworkStock) && !errors.Is(err, inventory.ErrInsufficientAvailableStock) {
						t.Fatalf("unexpected auto error: %v", err)
					}
					if j == 1 && !errors.Is(err, inventory.ErrInsufficientAvailableStock) && !errors.Is(err, ErrInsufficientNetworkStock) {
						t.Fatalf("unexpected manual reserve error: %v", err)
					}
				}
			}
			var reserved int64
			if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(quantity),0) FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2 AND status IN ('active','payment_held','committed')`, f.organizationID, f.inventoryID).Scan(&reserved); err != nil {
				t.Fatal(err)
			}
			if reserved > 10 {
				t.Fatalf("reserved=%d exceeds on_hand=10", reserved)
			}
			cleanupOrderFixture(t, pool, f.organizationID)
		})
	}
}

func TestAutomaticOrderVsAdjustmentRace(t *testing.T) {
	for i := 0; i < 20; i++ {
		t.Run(fmt.Sprintf("adjustment-%d", i), func(t *testing.T) {
			pool := orderPool(t)
			ctx := context.Background()
			f := newOrderFixture(t, pool, "Auto Adjustment", 10)
			repository := NewRepository(pool)
			inventoryRepo := inventory.NewRepository(pool)
			start := make(chan struct{})
			results := make(chan struct {
				source string
				err    error
			}, 2)
			go func() {
				<-start
				_, err := repository.AutoCreate(ctx, f.organizationID, f.userID, fmt.Sprintf("adjustment-auto-%d", i), strings.Repeat("a", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 7}}})
				results <- struct {
					source string
					err    error
				}{source: "automatic order", err: err}
			}()
			go func() {
				<-start
				_, err := inventoryRepo.Adjust(ctx, f.organizationID, f.inventoryID, -5)
				results <- struct {
					source string
					err    error
				}{source: "manual adjustment", err: err}
			}()
			close(start)
			var autoErr, adjustErr error
			for j := 0; j < 2; j++ {
				result := <-results
				if result.source == "automatic order" {
					autoErr = result.err
				} else {
					adjustErr = result.err
				}
			}
			if autoErr != nil && !errors.Is(autoErr, ErrInsufficientNetworkStock) && !errors.Is(autoErr, inventory.ErrInactiveWarehouse) && !errors.Is(autoErr, inventory.ErrInactiveProduct) {
				t.Fatalf("unexpected automatic error: %v", autoErr)
			}
			if adjustErr != nil && !errors.Is(adjustErr, inventory.ErrInsufficientStock) && !errors.Is(adjustErr, inventory.ErrStockBelowReserved) && !errors.Is(adjustErr, inventory.ErrNotFound) {
				if !errors.Is(adjustErr, ErrInsufficientNetworkStock) {
					t.Fatalf("unexpected adjustment error: %v", adjustErr)
				}
			}
			var onHand, active, paymentHeld, committed, consumed int64
			if err := pool.QueryRow(ctx, `SELECT i.on_hand_quantity, COALESCE(SUM(r.quantity) FILTER (WHERE r.status='active' AND r.expires_at > statement_timestamp()),0), COALESCE(SUM(r.quantity) FILTER (WHERE r.status='payment_held'),0), COALESCE(SUM(r.quantity) FILTER (WHERE r.status='committed'),0), COALESCE(SUM(r.quantity) FILTER (WHERE r.status='consumed'),0) FROM inventory_levels i LEFT JOIN inventory_reservations r ON r.organization_id=i.organization_id AND r.inventory_level_id=i.id WHERE i.organization_id=$1 AND i.id=$2 GROUP BY i.on_hand_quantity`, f.organizationID, f.inventoryID).Scan(&onHand, &active, &paymentHeld, &committed, &consumed); err != nil {
				t.Fatal(err)
			}
			reserved := active + paymentHeld + committed
			if reserved > onHand {
				t.Fatalf("reserved=%d exceeds on_hand=%d with autoErr=%v adjustErr=%v", reserved, onHand, autoErr, adjustErr)
			}
			var movementCount int
			var movementDelta *int64
			if err := pool.QueryRow(ctx, `SELECT COUNT(*), MIN(quantity_delta) FROM inventory_movements WHERE organization_id=$1 AND inventory_level_id=$2 AND movement_type='adjustment'`, f.organizationID, f.inventoryID).Scan(&movementCount, &movementDelta); err != nil {
				t.Fatal(err)
			}
			if adjustErr == nil && (movementCount != 1 || movementDelta == nil || *movementDelta != -5) {
				t.Fatalf("successful adjustment ledger mismatch: count=%d delta=%v", movementCount, movementDelta)
			}
			if adjustErr != nil && movementCount != 0 {
				t.Fatalf("failed adjustment left movement rows: count=%d delta=%v", movementCount, movementDelta)
			}
			var automaticOrders int
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM orders WHERE organization_id=$1 AND idempotency_key=$2`, f.organizationID, fmt.Sprintf("adjustment-auto-%d", i)).Scan(&automaticOrders); err != nil {
				t.Fatal(err)
			}
			var loggedMovementDelta any
			if movementDelta != nil {
				loggedMovementDelta = *movementDelta
			}
			t.Logf("final state: on_hand=%d active=%d payment_held=%d committed=%d consumed=%d effective_reserved=%d movement_count=%d movement_delta=%v automatic_orders=%d auto_err=%v adjustment_err=%v", onHand, active, paymentHeld, committed, consumed, reserved, movementCount, loggedMovementDelta, automaticOrders, autoErr, adjustErr)
			cleanupOrderFixture(t, pool, f.organizationID)
		})
	}
}

func TestAutomaticOrderReplayAfterStockChange(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	f := newOrderFixture(t, pool, "Auto Replay", 10)
	repository := NewRepository(pool)
	key := "replay-key"
	created, err := repository.AutoCreate(ctx, f.organizationID, f.userID, key, strings.Repeat("r", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	reservationIDs := make([]uuid.UUID, 0, len(created.Items))
	for _, item := range created.Items {
		if item.ReservationID != nil {
			reservationIDs = append(reservationIDs, *item.ReservationID)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_levels SET on_hand_quantity=1 WHERE organization_id=$1 AND id=$2`, f.organizationID, f.inventoryID); err != nil {
		t.Fatal(err)
	}
	replayed, err := repository.AutoCreate(ctx, f.organizationID, f.userID, key, strings.Repeat("r", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: f.productID, Quantity: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != created.ID {
		t.Fatalf("replay order ID mismatch: %s != %s", replayed.ID, created.ID)
	}
	if len(replayed.Items) != len(created.Items) {
		t.Fatalf("replay item count mismatch: %d != %d", len(replayed.Items), len(created.Items))
	}
	var replayReservationCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE organization_id=$1 AND order_item_id IN (SELECT id FROM order_items WHERE organization_id=$1 AND order_id=$2)`, f.organizationID, created.ID).Scan(&replayReservationCount); err != nil {
		t.Fatal(err)
	}
	if replayReservationCount != len(reservationIDs) {
		t.Fatalf("reservation count changed on replay: %d != %d", replayReservationCount, len(reservationIDs))
	}
}

func TestAutomaticOrderTenantIsolation(t *testing.T) {
	pool := orderPool(t)
	ctx := context.Background()
	orgA := uuid.New()
	orgB := uuid.New()
	userA := uuid.New()
	userB := uuid.New()
	productA := uuid.New()
	productB := uuid.New()
	warehouseA := uuid.New()
	warehouseB := uuid.New()
	inventoryA := uuid.New()
	inventoryB := uuid.New()
	for _, stmt := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO organizations (id,name,slug) VALUES ($1,$2,$3)`, []any{orgA, "OrgA", "orga-" + orgA.String()}},
		{`INSERT INTO organizations (id,name,slug) VALUES ($1,$2,$3)`, []any{orgB, "OrgB", "orgb-" + orgB.String()}},
		{`INSERT INTO users (id,email,password_hash) VALUES ($1,$2,'hash')`, []any{userA, "a-" + userA.String() + "@example.com"}},
		{`INSERT INTO users (id,email,password_hash) VALUES ($1,$2,'hash')`, []any{userB, "b-" + userB.String() + "@example.com"}},
		{`INSERT INTO organization_members (organization_id,user_id,role) VALUES ($1,$2,'owner')`, []any{orgA, userA}},
		{`INSERT INTO organization_members (organization_id,user_id,role) VALUES ($1,$2,'owner')`, []any{orgB, userB}},
		{`INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,$4,2000,'AUD')`, []any{productA, orgA, "A-" + productA.String(), "Org A Product"}},
		{`INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,$4,2000,'AUD')`, []any{productB, orgB, "B-" + productB.String(), "Org B Product"}},
		{`INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, []any{warehouseA, orgA, "WH-A", "Warehouse A"}},
		{`INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, []any{warehouseB, orgB, "WH-B", "Warehouse B"}},
		{`INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,1)`, []any{inventoryA, orgA, productA, warehouseA}},
		{`INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,100)`, []any{inventoryB, orgB, productB, warehouseB}},
	} {
		if _, err := pool.Exec(ctx, stmt.q, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanupOrderFixture(t, pool, orgA)
		cleanupOrderFixture(t, pool, orgB)
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1)`, []uuid.UUID{userA, userB}); err != nil {
			t.Errorf("clean up tenant test users: %v", err)
		}
	})
	repository := NewRepository(pool)
	_, err := repository.AutoCreate(ctx, orgA, userA, "tenant-cross", strings.Repeat("t", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: productB, Quantity: 2}}})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant product should be not found but got: %v", err)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE organization_id=$1`, orgA).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("orgA reservation count should be zero, got %d", rows)
	}
}

func TestAutomaticOrderUnexpectedDatabaseErrorPropagation(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	repo := NewRepository(pool)
	_, err = repo.AutoCreate(ctx, uuid.New(), uuid.New(), "closed-pool", strings.Repeat("x", 64), AutoCreateInput{Items: []AutoItemInput{{ProductID: uuid.New(), Quantity: 1}}})
	if err == nil {
		t.Fatal("expected closed-pool request to fail")
	}
}
