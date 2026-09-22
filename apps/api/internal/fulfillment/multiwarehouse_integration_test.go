package fulfillment_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"flowcart/apps/api/internal/fulfillment"
	"flowcart/apps/api/internal/order"
	"github.com/google/uuid"
)

func TestMultiwarehousePartialAndConcurrentFinalFulfillment(t *testing.T) {
	pool := fulfillmentPool(t)
	ctx := context.Background()
	fixture := newFulfillmentFixture(t, pool)
	productB, warehouseB, inventoryB := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,'Second Product',2000,'AUD')`, productB, fixture.org, "FUL-"+productB.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,'Second Warehouse')`, warehouseB, fixture.org, "FUL-"+warehouseB.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,10)`, inventoryB, fixture.org, productB, warehouseB); err != nil {
		t.Fatal(err)
	}
	created, err := order.NewRepository(pool).Create(ctx, fixture.org, fixture.user, "multi-order-"+uuid.NewString(), strings.Repeat("m", 64), order.CreateInput{Items: []order.ItemInput{{InventoryLevelID: fixture.inventory, Quantity: 2}, {InventoryLevelID: inventoryB, Quantity: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='paid',paid_at=NOW() WHERE organization_id=$1 AND id=$2`, fixture.org, created.ID); err != nil {
		t.Fatal(err)
	}
	var reservationA, reservationB uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT r.id FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE r.organization_id=$1 AND oi.order_id=$2 AND r.inventory_level_id=$3`, fixture.org, created.ID, fixture.inventory).Scan(&reservationA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT r.id FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE r.organization_id=$1 AND oi.order_id=$2 AND r.inventory_level_id=$3`, fixture.org, created.ID, inventoryB).Scan(&reservationB); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_reservations SET status='committed' WHERE organization_id=$1 AND id IN ($2,$3)`, fixture.org, reservationA, reservationB); err != nil {
		t.Fatal(err)
	}
	service := fulfillment.NewService(fulfillment.NewRepository(pool))
	if _, err := service.Complete(ctx, tenant(fixture), created.ID, "mixed-warehouse", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationA, reservationB}}); !errors.Is(err, fulfillment.ErrMixedWarehouse) {
		t.Fatalf("mixed warehouse=%v", err)
	}
	first, err := service.Complete(ctx, tenant(fixture), created.ID, "warehouse-a", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationA}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != fulfillment.StatusCompleted {
		t.Fatalf("first fulfillment=%+v", first)
	}
	var orderStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE organization_id=$1 AND id=$2`, fixture.org, created.ID).Scan(&orderStatus); err != nil {
		t.Fatal(err)
	}
	if orderStatus != "partially_fulfilled" {
		t.Fatalf("partial order status=%s", orderStatus)
	}

	secondFixture := newFulfillmentFixture(t, pool)
	productB2, warehouseB2, inventoryB2 := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,'Concurrent Product',2000,'AUD')`, productB2, secondFixture.org, "FUL-"+productB2.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,'Concurrent Warehouse')`, warehouseB2, secondFixture.org, "FUL-"+warehouseB2.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,10)`, inventoryB2, secondFixture.org, productB2, warehouseB2); err != nil {
		t.Fatal(err)
	}
	concurrentOrder, err := order.NewRepository(pool).Create(ctx, secondFixture.org, secondFixture.user, "concurrent-order-"+uuid.NewString(), strings.Repeat("n", 64), order.CreateInput{Items: []order.ItemInput{{InventoryLevelID: secondFixture.inventory, Quantity: 2}, {InventoryLevelID: inventoryB2, Quantity: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='paid',paid_at=NOW() WHERE organization_id=$1 AND id=$2`, secondFixture.org, concurrentOrder.ID); err != nil {
		t.Fatal(err)
	}
	var reservationC, reservationD uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT r.id FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE r.organization_id=$1 AND oi.order_id=$2 AND r.inventory_level_id=$3`, secondFixture.org, concurrentOrder.ID, secondFixture.inventory).Scan(&reservationC); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT r.id FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE r.organization_id=$1 AND oi.order_id=$2 AND r.inventory_level_id=$3`, secondFixture.org, concurrentOrder.ID, inventoryB2).Scan(&reservationD); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_reservations SET status='committed' WHERE organization_id=$1 AND id IN ($2,$3)`, secondFixture.org, reservationC, reservationD); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, err := service.Complete(ctx, tenant(secondFixture), concurrentOrder.ID, "concurrent-a", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationC}})
		results <- err
	}()
	go func() {
		defer wait.Done()
		_, err := service.Complete(ctx, tenant(secondFixture), concurrentOrder.ID, "concurrent-b", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationD}})
		results <- err
	}()
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil && !errors.Is(err, fulfillment.ErrOrderAlreadyFulfilled) {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE organization_id=$1 AND id=$2`, secondFixture.org, concurrentOrder.ID).Scan(&orderStatus); err != nil {
		t.Fatal(err)
	}
	if orderStatus != "fulfilled" {
		t.Fatalf("concurrent final order status=%s", orderStatus)
	}
}
