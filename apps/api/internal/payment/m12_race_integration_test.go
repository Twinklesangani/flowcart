package payment_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"flowcart/apps/api/internal/order"
	"flowcart/apps/api/internal/payment"
)

func TestSuccessAndProviderBackedCancellationRace(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	paymentRepo := payment.NewRepository(pool)
	created, err := paymentRepo.CreateProvider(ctx, f.org, f.user, ord.ID, "success-cancel-race", strings.Repeat("4", 64), false)
	if err != nil {
		t.Fatal(err)
	}
	providerID := "pi_success_cancel_race"
	if _, err := pool.Exec(ctx, `UPDATE payments SET provider_payment_id=$2,provider_status='requires_payment_method' WHERE organization_id=$1 AND id=$3`, f.org, providerID, created.ID); err != nil {
		t.Fatal(err)
	}
	event := payment.VerifiedEvent{ID: "evt_success_cancel_race", Type: "payment_intent.succeeded", Provider: "stripe", IntentID: providerID, Status: "succeeded", Currency: "AUD", Amount: 2000, AmountReceived: 2000, Livemode: false, PayloadHash: strings.Repeat("5", 64), CreatedAt: time.Now()}
	if err := paymentRepo.AcceptEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	orderRepo := order.NewRepository(pool)
	var wait sync.WaitGroup
	wait.Add(2)
	results := make(chan error, 2)
	go func() { defer wait.Done(); results <- paymentRepo.ProcessEvents(ctx) }()
	go func() { defer wait.Done(); _, cancelErr := orderRepo.Cancel(ctx, f.org, ord.ID); results <- cancelErr }()
	wait.Wait()
	close(results)
	for raceErr := range results {
		if raceErr != nil && !errors.Is(raceErr, payment.ErrPaymentAlreadySucceeded) {
			t.Fatal(raceErr)
		}
	}

	var paymentStatus, orderStatus, reservationStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM payments WHERE organization_id=$1 AND id=$2`, f.org, created.ID).Scan(&paymentStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE organization_id=$1 AND id=$2`, f.org, ord.ID).Scan(&orderStatus); err != nil {
		t.Fatal(err)
	}
	if paymentStatus == payment.StatusPending {
		current, getErr := paymentRepo.Get(ctx, f.org, created.ID)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if current.CancelRequestedAt == nil {
			t.Fatal("cancellation did not persist its request")
		}
		cancelled := payment.ProviderIntent{ID: providerID, Status: "canceled", Amount: 2000, AmountReceived: 0, Currency: "AUD", Livemode: false}
		if _, err := paymentRepo.ConfirmCancelled(ctx, current, cancelled); err != nil {
			t.Fatal(err)
		}
		paymentStatus = payment.StatusCancelled
		orderStatus = "cancelled"
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2`, f.org, f.inventory).Scan(&reservationStatus); err != nil {
		t.Fatal(err)
	}
	if paymentStatus == payment.StatusSucceeded {
		if orderStatus != "paid" || reservationStatus != "committed" {
			t.Fatalf("success outcome payment=%s order=%s reservation=%s", paymentStatus, orderStatus, reservationStatus)
		}
	} else if paymentStatus == payment.StatusCancelled {
		if orderStatus != "cancelled" || (reservationStatus != "released" && reservationStatus != "expired") {
			t.Fatalf("cancel outcome payment=%s order=%s reservation=%s", paymentStatus, orderStatus, reservationStatus)
		}
	} else {
		t.Fatalf("unexpected terminal outcome payment=%s order=%s reservation=%s", paymentStatus, orderStatus, reservationStatus)
	}
	if paymentStatus == payment.StatusSucceeded && orderStatus == "cancelled" {
		t.Fatal("mixed paid and cancelled state")
	}
}

func TestProviderFailureAndNewPaymentAttemptRace(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	repo := payment.NewRepository(pool)
	created, err := repo.CreateProvider(ctx, f.org, f.user, ord.ID, "failure-attempt-race", strings.Repeat("6", 64), false)
	if err != nil {
		t.Fatal(err)
	}
	providerID := "pi_failure_attempt_race"
	if _, err := pool.Exec(ctx, `UPDATE payments SET provider_payment_id=$2,provider_status='requires_payment_method' WHERE organization_id=$1 AND id=$3`, f.org, providerID, created.ID); err != nil {
		t.Fatal(err)
	}
	event := payment.VerifiedEvent{ID: "evt_failure_attempt_race", Type: "payment_intent.payment_failed", Provider: "stripe", IntentID: providerID, Status: "requires_payment_method", Currency: "AUD", Amount: 2000, AmountReceived: 0, Livemode: false, PayloadHash: strings.Repeat("7", 64), CreatedAt: time.Now()}
	if err := repo.AcceptEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	wait.Add(2)
	attemptErrors := make(chan error, 1)
	go func() {
		defer wait.Done()
		attemptErrors <- func() error {
			_, attemptErr := repo.Create(ctx, f.org, f.user, ord.ID, "second-race-attempt", strings.Repeat("8", 64))
			return attemptErr
		}()
	}()
	go func() { defer wait.Done(); _ = repo.ProcessEvents(ctx) }()
	wait.Wait()
	attemptErr := <-attemptErrors
	if attemptErr == nil || (!errors.Is(attemptErr, payment.ErrPaymentInProgress) && !errors.Is(attemptErr, payment.ErrPaymentAlreadySucceeded) && !errors.Is(attemptErr, payment.ErrReservationExpired)) {
		t.Fatalf("new attempt error=%v", attemptErr)
	}
	current, err := repo.Get(ctx, f.org, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.CancelRequestedAt == nil {
		t.Fatal("failure did not request cancellation")
	}
	cancelled := payment.ProviderIntent{ID: providerID, Status: "canceled", Amount: 2000, Currency: "AUD", Livemode: false}
	if _, err := repo.ConfirmCancelled(ctx, current, cancelled); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM payments WHERE organization_id=$1 AND order_id=$2`, f.org, ord.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("payment attempts=%d", count)
	}
}
