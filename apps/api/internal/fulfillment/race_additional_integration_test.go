package fulfillment_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"flowcart/apps/api/internal/fulfillment"
	"flowcart/apps/api/internal/inventory"
	"flowcart/apps/api/internal/payment"
	"github.com/google/uuid"
)

func TestAdjustmentAndFulfillmentRacePreservesStockInvariant(t *testing.T) {
	pool := fulfillmentPool(t)
	ctx := context.Background()
	fixture := newFulfillmentFixture(t, pool)
	created := paidOrder(t, pool, fixture, 4)
	reservationID := *created.Items[0].ReservationID
	service := fulfillment.NewService(fulfillment.NewRepository(pool))
	results := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, err := service.Complete(ctx, tenant(fixture), created.ID, "adjust-race-fulfill", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationID}})
		results <- err
	}()
	go func() {
		defer wait.Done()
		_, err := inventory.NewRepository(pool).Adjust(ctx, fixture.org, fixture.inventory, -7)
		results <- err
	}()
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil && !errors.Is(err, inventory.ErrStockBelowReserved) && !errors.Is(err, inventory.ErrInsufficientStock) {
			t.Fatal(err)
		}
	}
	var onHand int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE organization_id=$1 AND id=$2`, fixture.org, fixture.inventory).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != 6 {
		t.Fatalf("on hand=%d", onHand)
	}
	var movements int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE organization_id=$1 AND inventory_level_id=$2 AND movement_type='adjustment'`, fixture.org, fixture.inventory).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if movements != 0 {
		t.Fatalf("adjustment movements=%d", movements)
	}
}

func TestPaymentSuccessAndFulfillmentRaceRequiresCommittedReservation(t *testing.T) {
	pool := fulfillmentPool(t)
	ctx := context.Background()
	fixture := newFulfillmentFixture(t, pool)
	created := paidOrder(t, pool, fixture, 4)
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='pending',paid_at=NULL WHERE organization_id=$1 AND id=$2`, fixture.org, created.ID); err != nil {
		t.Fatal(err)
	}
	reservationID := *created.Items[0].ReservationID
	if _, err := pool.Exec(ctx, `UPDATE inventory_reservations SET status='active' WHERE organization_id=$1 AND id=$2`, fixture.org, reservationID); err != nil {
		t.Fatal(err)
	}
	paymentRepo := payment.NewRepository(pool)
	attempt, err := paymentRepo.CreateProvider(ctx, fixture.org, fixture.user, created.ID, "payment-fulfill-race", strings.Repeat("q", 64), false)
	if err != nil {
		t.Fatal(err)
	}
	providerID := "pi_payment_fulfill_race"
	if _, err := pool.Exec(ctx, `UPDATE payments SET provider_payment_id=$2,provider_status='requires_payment_method' WHERE organization_id=$1 AND id=$3`, fixture.org, providerID, attempt.ID); err != nil {
		t.Fatal(err)
	}
	event := payment.VerifiedEvent{ID: "evt_payment_fulfill_race", Type: "payment_intent.succeeded", Provider: "stripe", IntentID: providerID, Status: "succeeded", Currency: "AUD", Amount: 8000, AmountReceived: 8000, Livemode: false, PayloadHash: strings.Repeat("r", 64), CreatedAt: time.Now()}
	if err := paymentRepo.AcceptEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	service := fulfillment.NewService(fulfillment.NewRepository(pool))
	results := make(chan error, 1)
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		_, err := service.Complete(ctx, tenant(fixture), created.ID, "payment-fulfill", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationID}})
		results <- err
	}()
	if err := paymentRepo.ProcessEvents(ctx); err != nil {
		t.Fatal(err)
	}
	wait.Wait()
	fulfillmentErr := <-results
	if fulfillmentErr != nil && !errors.Is(fulfillmentErr, fulfillment.ErrOrderNotPaid) && !errors.Is(fulfillmentErr, fulfillment.ErrReservationNotCommitted) {
		t.Fatal(fulfillmentErr)
	}
	if err := paymentRepo.ProcessEvents(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(ctx, tenant(fixture), created.ID, "payment-fulfill-retry", fulfillment.CreateInput{ReservationIDs: []uuid.UUID{reservationID}}); err != nil && !errors.Is(err, fulfillment.ErrOrderAlreadyFulfilled) {
		t.Fatal(err)
	}
	var onHand int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE organization_id=$1 AND id=$2`, fixture.org, fixture.inventory).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != 6 {
		t.Fatalf("on hand=%d", onHand)
	}
}
