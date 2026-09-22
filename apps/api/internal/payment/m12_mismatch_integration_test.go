package payment_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"flowcart/apps/api/internal/payment"
)

func TestTrustedSuccessMismatchesQuarantineAndRetainHold(t *testing.T) {
	cases := []struct {
		name       string
		amount     int64
		received   int64
		currency   string
		livemode   bool
		providerID string
	}{
		{name: "amount", amount: 2001, received: 2001, currency: "AUD", providerID: "pi_amount"},
		{name: "received", amount: 2000, received: 1999, currency: "AUD", providerID: "pi_received"},
		{name: "currency", amount: 2000, received: 2000, currency: "USD", providerID: "pi_currency"},
		{name: "livemode", amount: 2000, received: 2000, currency: "AUD", livemode: true, providerID: "pi_livemode"},
		{name: "provider identity", amount: 2000, received: 2000, currency: "AUD", providerID: "pi_wrong_identity"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			pool := poolForPayment(t)
			ctx := context.Background()
			f := paymentFixture(t, pool, 2000)
			t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
			ord := createPaymentOrder(t, pool, f, 1)
			repo := payment.NewRepository(pool)
			created, err := repo.CreateProvider(ctx, f.org, f.user, ord.ID, "mismatch-"+testCase.name, strings.Repeat("e", 64), false)
			if err != nil {
				t.Fatal(err)
			}
			providerID := "pi_expected"
			if testCase.name == "provider identity" {
				providerID = "pi_bound"
			}
			if _, err := pool.Exec(ctx, `UPDATE payments SET provider_payment_id=$2,provider_status='requires_payment_method' WHERE organization_id=$1 AND id=$3`, f.org, providerID, created.ID); err != nil {
				t.Fatal(err)
			}
			event := payment.VerifiedEvent{ID: "evt_mismatch_" + testCase.name, Type: "payment_intent.succeeded", Provider: "stripe", IntentID: testCase.providerID, Status: "succeeded", Currency: testCase.currency, Amount: testCase.amount, AmountReceived: testCase.received, Livemode: testCase.livemode, PayloadHash: strings.Repeat("f", 64), CreatedAt: time.Now()}
			if testCase.name != "provider identity" {
				event.IntentID = providerID
			}
			if err := repo.AcceptEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
			if err := repo.ProcessEvents(ctx); err != nil {
				t.Fatal(err)
			}
			var status, reason, reservation string
			if err := pool.QueryRow(ctx, `SELECT status,COALESCE(attention_reason,'') FROM payments WHERE organization_id=$1 AND id=$2`, f.org, created.ID).Scan(&status, &reason); err != nil {
				t.Fatal(err)
			}
			if status != payment.StatusPending {
				t.Fatalf("payment status=%s", status)
			}
			if testCase.name == "provider identity" {
				var outcome, eventReason string
				if err := pool.QueryRow(ctx, `SELECT outcome,COALESCE(reason_code,'') FROM payment_provider_events WHERE provider_event_id=$1`, event.ID).Scan(&outcome, &eventReason); err != nil {
					t.Fatal(err)
				}
				if outcome != "pending" || eventReason != "binding_unresolved" {
					t.Fatalf("unresolved event=%s reason=%s", outcome, eventReason)
				}
			} else if reason == "" {
				t.Fatalf("payment attention reason is empty for %s", testCase.name)
			}
			if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE organization_id=$1 AND id=$2`, f.org, ord.ID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "pending" {
				t.Fatalf("order status=%s", status)
			}
			if err := pool.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2`, f.org, f.inventory).Scan(&reservation); err != nil {
				t.Fatal(err)
			}
			if reservation != "payment_held" {
				t.Fatalf("reservation status=%s", reservation)
			}
			if _, err := repo.Create(ctx, f.org, f.user, ord.ID, "unsafe-second-attempt", strings.Repeat("1", 64)); err == nil {
				t.Fatal("mismatch allowed a second attempt")
			}
		})
	}
}

func TestLateTrustedSuccessCommitsRetainedPaymentHold(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	repo := payment.NewRepository(pool)
	created, err := repo.CreateProvider(ctx, f.org, f.user, ord.ID, "late-success", strings.Repeat("2", 64), false)
	if err != nil {
		t.Fatal(err)
	}
	providerID := "pi_late_success"
	if _, err := pool.Exec(ctx, `UPDATE payments SET provider_payment_id=$2,provider_status='requires_payment_method',checkout_deadline_at=NOW()-INTERVAL '1 minute' WHERE organization_id=$1 AND id=$3`, f.org, providerID, created.ID); err != nil {
		t.Fatal(err)
	}
	event := payment.VerifiedEvent{ID: "evt_late_success", Type: "payment_intent.succeeded", Provider: "stripe", IntentID: providerID, Status: "succeeded", Currency: "AUD", Amount: 2000, AmountReceived: 2000, Livemode: false, PayloadHash: strings.Repeat("3", 64), CreatedAt: time.Now()}
	if err := repo.AcceptEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := repo.ProcessEvents(ctx); err != nil {
		t.Fatal(err)
	}
	assertPaymentOrderReservation(t, pool, f.org, created.ID, ord.ID, f.inventory, "succeeded", "paid", "committed")
}
