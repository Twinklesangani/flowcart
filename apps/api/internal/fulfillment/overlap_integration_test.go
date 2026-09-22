package fulfillment_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"flowcart/apps/api/internal/fulfillment"
	"github.com/google/uuid"
)

func TestOverlappingFulfillmentsConsumeReservationOnce(t *testing.T) {
	pool := fulfillmentPool(t)
	ctx := context.Background()
	fixture := newFulfillmentFixture(t, pool)
	created := paidOrder(t, pool, fixture, 4)
	firstReservation := *created.Items[0].ReservationID
	secondReservation := uuid.New()
	if _, err := pool.Exec(ctx, `UPDATE inventory_reservations SET quantity=2,status='committed' WHERE organization_id=$1 AND id=$2`, fixture.org, firstReservation); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_reservations (id,organization_id,inventory_level_id,order_item_id,quantity,status,expires_at) VALUES ($1,$2,$3,$4,2,'committed',$5)`, secondReservation, fixture.org, fixture.inventory, created.Items[0].ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	service := fulfillment.NewService(fulfillment.NewRepository(pool))
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, err := service.Complete(ctx, tenant(fixture), created.ID, "overlap-a", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{firstReservation, secondReservation}})
		results <- err
	}()
	go func() {
		defer wait.Done()
		_, err := service.Complete(ctx, tenant(fixture), created.ID, "overlap-b", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{secondReservation}})
		results <- err
	}()
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
	var onHand int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE organization_id=$1 AND id=$2`, fixture.org, fixture.inventory).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != 6 && onHand != 8 {
		t.Fatalf("on hand=%d", onHand)
	}
	var movementCount int
	var movementTotal int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*),COALESCE(SUM(-quantity_delta),0) FROM inventory_movements WHERE organization_id=$1 AND movement_type='fulfillment'`, fixture.org).Scan(&movementCount, &movementTotal); err != nil {
		t.Fatal(err)
	}
	if movementCount != 1 && movementCount != 2 {
		t.Fatalf("movement count=%d", movementCount)
	}
	if movementTotal != 10-onHand {
		t.Fatalf("movement total=%d on hand=%d", movementTotal, onHand)
	}
}

func TestFailedFulfillmentRollsBackStockReservationsAndMovements(t *testing.T) {
	pool := fulfillmentPool(t)
	ctx := context.Background()
	fixture := newFulfillmentFixture(t, pool)
	created := paidOrder(t, pool, fixture, 4)
	reservationID := *created.Items[0].ReservationID
	if _, err := pool.Exec(ctx, `UPDATE inventory_levels SET on_hand_quantity=2 WHERE organization_id=$1 AND id=$2`, fixture.org, fixture.inventory); err != nil {
		t.Fatal(err)
	}
	service := fulfillment.NewService(fulfillment.NewRepository(pool))
	if _, err := service.Complete(ctx, tenant(fixture), created.ID, "insufficient-stock", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationID}}); !errors.Is(err, fulfillment.ErrInsufficientStock) {
		t.Fatalf("error=%v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE organization_id=$1 AND id=$2`, fixture.org, reservationID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "committed" {
		t.Fatalf("reservation status=%s", status)
	}
	var movementCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE organization_id=$1`, fixture.org).Scan(&movementCount); err != nil {
		t.Fatal(err)
	}
	if movementCount != 0 {
		t.Fatalf("movement count=%d", movementCount)
	}
}
