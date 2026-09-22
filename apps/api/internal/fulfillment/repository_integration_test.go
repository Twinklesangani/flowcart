package fulfillment_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"flowcart/apps/api/internal/fulfillment"
	"flowcart/apps/api/internal/inventory"
	"flowcart/apps/api/internal/order"
	"flowcart/apps/api/internal/organization"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
)

type fulfillmentFixture struct{ org, user, product, warehouse, inventory uuid.UUID }

func fulfillmentPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
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

func newFulfillmentFixture(t *testing.T, pool *pgxpool.Pool) fulfillmentFixture {
	t.Helper()
	ctx := context.Background()
	fixture := fulfillmentFixture{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	_, err := pool.Exec(ctx, `INSERT INTO organizations (id,name,slug) VALUES ($1,'Fulfillment Test',$2)`, fixture.org, "fulfillment-"+fixture.org.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO users (id,email,password_hash) VALUES ($1,$2,'test')`, fixture.user, "fulfillment-"+fixture.user.String()+"@example.com")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO organization_members (organization_id,user_id,role) VALUES ($1,$2,'owner')`, fixture.org, fixture.user)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,'Fulfillment Product',2000,'AUD')`, fixture.product, fixture.org, "FUL-"+fixture.product.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,'Fulfillment Warehouse')`, fixture.warehouse, fixture.org, "FUL-"+fixture.warehouse.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,10)`, fixture.inventory, fixture.org, fixture.product, fixture.warehouse)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupFulfillmentFixture(t, pool, fixture) })
	return fixture
}

func cleanupFulfillmentFixture(t *testing.T, pool *pgxpool.Pool, fixture fulfillmentFixture) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Errorf("begin fulfillment cleanup: %v", err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `ALTER TABLE audit_events DISABLE TRIGGER audit_events_append_only_trg`); err != nil {
		t.Errorf("disable audit append-only trigger during fulfillment cleanup: %v", err)
		return
	}
	for _, query := range []string{
		`DELETE FROM audit_events WHERE organization_id=$1`,
		`DELETE FROM inventory_movements WHERE organization_id=$1`,
		`DELETE FROM fulfillments WHERE organization_id=$1`,
		`DELETE FROM inventory_levels WHERE organization_id=$1`,
		`DELETE FROM orders WHERE organization_id=$1`,
		`DELETE FROM organizations WHERE id=$1`,
		`DELETE FROM users WHERE id=$1`,
	} {
		if _, err := tx.Exec(ctx, query, fixture.org); err != nil {
			t.Errorf("cleanup fulfillment fixture: %v", err)
			return
		}
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE audit_events ENABLE TRIGGER audit_events_append_only_trg`); err != nil {
		t.Errorf("re-enable audit append-only trigger after fulfillment cleanup: %v", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		t.Errorf("commit fulfillment cleanup: %v", err)
	}
}

func paidOrder(t *testing.T, pool *pgxpool.Pool, fixture fulfillmentFixture, quantity int64) order.Order {
	t.Helper()
	created, err := order.NewRepository(pool).Create(context.Background(), fixture.org, fixture.user, "order-"+uuid.NewString(), strings.Repeat("a", 64), order.CreateInput{Items: []order.ItemInput{{InventoryLevelID: fixture.inventory, Quantity: quantity}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE orders SET status='paid',paid_at=NOW() WHERE organization_id=$1 AND id=$2`, fixture.org, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE inventory_reservations SET status='committed' WHERE organization_id=$1 AND order_item_id=$2`, fixture.org, created.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	return created
}

func tenant(f fulfillmentFixture) organization.TenantContext {
	return organization.TenantContext{OrganizationID: f.org, UserID: f.user, Role: organization.RoleOwner}
}

func TestFulfillmentConsumesCommittedReservationAndWritesLedger(t *testing.T) {
	pool := fulfillmentPool(t)
	ctx := context.Background()
	fixture := newFulfillmentFixture(t, pool)
	created := paidOrder(t, pool, fixture, 4)
	reservationID := *created.Items[0].ReservationID
	service := fulfillment.NewService(fulfillment.NewRepository(pool))
	result, err := service.Complete(ctx, tenant(fixture), created.ID, "fulfill-once", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationID}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != fulfillment.StatusCompleted || len(result.Items) != 1 || result.Items[0].Quantity != 4 {
		t.Fatalf("result=%+v", result)
	}
	var onHand int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE organization_id=$1 AND id=$2`, fixture.org, fixture.inventory).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != 6 {
		t.Fatalf("on hand=%d", onHand)
	}
	var reservationStatus, orderStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE organization_id=$1 AND id=$2`, fixture.org, reservationID).Scan(&reservationStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE organization_id=$1 AND id=$2`, fixture.org, created.ID).Scan(&orderStatus); err != nil {
		t.Fatal(err)
	}
	if reservationStatus != "consumed" || orderStatus != "fulfilled" {
		t.Fatalf("reservation=%s order=%s", reservationStatus, orderStatus)
	}
	var movementDelta int64
	if err := pool.QueryRow(ctx, `SELECT quantity_delta FROM inventory_movements WHERE organization_id=$1 AND reservation_id=$2`, fixture.org, reservationID).Scan(&movementDelta); err != nil {
		t.Fatal(err)
	}
	if movementDelta != -4 {
		t.Fatalf("movement delta=%d", movementDelta)
	}
	level, err := inventory.NewRepository(pool).Get(ctx, fixture.org, fixture.inventory)
	if err != nil {
		t.Fatal(err)
	}
	if level.ReservedQuantity != 0 || level.AvailableQuantity != 6 {
		t.Fatalf("availability=%+v", level)
	}
}

func TestFulfillmentValidationAndTenantIsolation(t *testing.T) {
	pool := fulfillmentPool(t)
	ctx := context.Background()
	fixture := newFulfillmentFixture(t, pool)
	other := newFulfillmentFixture(t, pool)
	created := paidOrder(t, pool, fixture, 1)
	reservationID := *created.Items[0].ReservationID
	service := fulfillment.NewService(fulfillment.NewRepository(pool))
	if _, err := service.Complete(ctx, tenant(other), created.ID, "foreign", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationID}}); !errors.Is(err, fulfillment.ErrReservationNotFound) {
		t.Fatalf("foreign reservation=%v", err)
	}
	if _, err := service.Complete(ctx, tenant(fixture), created.ID, "empty", fulfillment.CreateInput{}); !errors.Is(err, fulfillment.ErrInvalidInput) {
		t.Fatalf("empty input=%v", err)
	}
	if _, err := service.Complete(ctx, tenant(fixture), created.ID, "duplicate", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationID, reservationID}}); !errors.Is(err, fulfillment.ErrInvalidInput) {
		t.Fatalf("duplicate input=%v", err)
	}
	pendingFixture := newFulfillmentFixture(t, pool)
	pendingOrder := paidOrder(t, pool, pendingFixture, 1)
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='pending',paid_at=NULL WHERE organization_id=$1 AND id=$2`, pendingFixture.org, pendingOrder.ID); err != nil {
		t.Fatal(err)
	}
	pendingReservation := *pendingOrder.Items[0].ReservationID
	if _, err := service.Complete(ctx, tenant(pendingFixture), pendingOrder.ID, "pending-order", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{pendingReservation}}); !errors.Is(err, fulfillment.ErrOrderNotPaid) {
		t.Fatalf("pending order=%v", err)
	}

	heldFixture := newFulfillmentFixture(t, pool)
	heldOrder := paidOrder(t, pool, heldFixture, 1)
	heldReservation := *heldOrder.Items[0].ReservationID
	if _, err := pool.Exec(ctx, `UPDATE inventory_reservations SET status='payment_held' WHERE organization_id=$1 AND id=$2`, heldFixture.org, heldReservation); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(ctx, tenant(heldFixture), heldOrder.ID, "held-reservation", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{heldReservation}}); !errors.Is(err, fulfillment.ErrReservationNotCommitted) {
		t.Fatalf("held reservation=%v", err)
	}

	if _, err := service.Complete(ctx, tenant(fixture), created.ID, "first", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(ctx, tenant(fixture), created.ID, "first", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{uuid.New()}}); !errors.Is(err, fulfillment.ErrIdempotencyKeyReused) {
		t.Fatalf("different replay=%v", err)
	}
	if _, err := service.Complete(ctx, tenant(fixture), created.ID, "second", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationID}}); !errors.Is(err, fulfillment.ErrOrderAlreadyFulfilled) && !errors.Is(err, fulfillment.ErrReservationAlreadyConsumed) {
		t.Fatalf("consumed input=%v", err)
	}
	if _, err := service.List(ctx, tenant(other), created.ID); !errors.Is(err, fulfillment.ErrOrderNotFound) {
		t.Fatalf("foreign list=%v", err)
	}
	if _, err := service.Movements(ctx, tenant(other), fixture.inventory, 50); !errors.Is(err, fulfillment.ErrNotFound) {
		t.Fatalf("foreign movements=%v", err)
	}
}

func TestFulfillmentSameKeyReplayAndSameReservationRace(t *testing.T) {
	pool := fulfillmentPool(t)
	ctx := context.Background()
	fixture := newFulfillmentFixture(t, pool)
	created := paidOrder(t, pool, fixture, 4)
	reservationID := *created.Items[0].ReservationID
	service := fulfillment.NewService(fulfillment.NewRepository(pool))
	first, err := service.Complete(ctx, tenant(fixture), created.ID, "same-key", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationID}})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Complete(ctx, tenant(fixture), created.ID, "same-key", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationID}})
	if err != nil || replay.ID != first.ID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}

	secondFixture := newFulfillmentFixture(t, pool)
	secondOrder := paidOrder(t, pool, secondFixture, 4)
	secondReservation := *secondOrder.Items[0].ReservationID
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wait.Done()
			_, err := service.Complete(ctx, tenant(secondFixture), secondOrder.ID, uuid.NewString(), fulfillment.CreateInput{ReservationIDs: []uuid.UUID{secondReservation}})
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, fulfillment.ErrOrderAlreadyFulfilled) && !errors.Is(err, fulfillment.ErrReservationAlreadyConsumed) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d", successes)
	}
}

func TestManualAdjustmentWritesMovement(t *testing.T) {
	pool := fulfillmentPool(t)
	ctx := context.Background()
	fixture := newFulfillmentFixture(t, pool)
	if _, err := inventory.NewRepository(pool).Adjust(ctx, fixture.org, fixture.inventory, 2); err != nil {
		t.Fatal(err)
	}
	var movementType string
	var delta int64
	if err := pool.QueryRow(ctx, `SELECT movement_type,quantity_delta FROM inventory_movements WHERE organization_id=$1 AND inventory_level_id=$2`, fixture.org, fixture.inventory).Scan(&movementType, &delta); err != nil {
		t.Fatal(err)
	}
	if movementType != "adjustment" || delta != 2 {
		t.Fatalf("movement=%s delta=%d", movementType, delta)
	}
}
