package payment_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"flowcart/apps/api/internal/payment"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTrustedSuccessCommitsPaymentOrderAndReservationOnce(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	repo := payment.NewRepository(pool)
	created, err := repo.CreateProvider(ctx, f.org, f.user, ord.ID, "trusted-success", strings.Repeat("a", 64), false)
	if err != nil {
		t.Fatal(err)
	}
	providerID := "pi_trusted_test"
	if _, err := pool.Exec(ctx, `UPDATE payments SET provider_payment_id=$2,provider_status='requires_payment_method' WHERE organization_id=$1 AND id=$3`, f.org, providerID, created.ID); err != nil {
		t.Fatal(err)
	}
	beforeOnHand := int64(0)
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE organization_id=$1 AND id=$2`, f.org, f.inventory).Scan(&beforeOnHand); err != nil {
		t.Fatal(err)
	}
	event := payment.VerifiedEvent{ID: "evt_trusted_test", Type: "payment_intent.succeeded", Provider: "stripe", IntentID: providerID, Status: "succeeded", Currency: "AUD", Amount: 2000, AmountReceived: 2000, Livemode: false, PayloadHash: strings.Repeat("b", 64), CreatedAt: time.Now()}
	if err := repo.AcceptEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := repo.ProcessEvents(ctx); err != nil {
		t.Fatal(err)
	}
	assertTrustedSuccessState(t, pool, f.org, created.ID, ord.ID, f.inventory, beforeOnHand)
	if err := repo.AcceptEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := repo.ProcessEvents(ctx); err != nil {
		t.Fatal(err)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM payment_provider_events WHERE provider_name='stripe' AND provider_event_id=$1`, event.ID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("event rows=%d", eventCount)
	}
}

func TestProviderEventBeforeBindingIsRetriedAfterBinding(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	repo := payment.NewRepository(pool)
	created, err := repo.CreateProvider(ctx, f.org, f.user, ord.ID, "before-binding", strings.Repeat("c", 64), false)
	if err != nil {
		t.Fatal(err)
	}
	providerID := "pi_before_binding"
	event := payment.VerifiedEvent{ID: "evt_before_binding", Type: "payment_intent.succeeded", Provider: "stripe", IntentID: providerID, Status: "succeeded", Currency: "AUD", Amount: 2000, AmountReceived: 2000, Livemode: false, PayloadHash: strings.Repeat("d", 64), CreatedAt: time.Now()}
	if err := repo.AcceptEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := repo.ProcessEvents(ctx); err != nil {
		t.Fatal(err)
	}
	var outcome, reason string
	if err := pool.QueryRow(ctx, `SELECT outcome,reason_code FROM payment_provider_events WHERE provider_event_id=$1`, event.ID).Scan(&outcome, &reason); err != nil {
		t.Fatal(err)
	}
	if outcome != "pending" || reason != "binding_unresolved" {
		t.Fatalf("before binding outcome=%s reason=%s", outcome, reason)
	}
	if _, err := pool.Exec(ctx, `UPDATE payments SET provider_payment_id=$2,provider_status='requires_payment_method' WHERE organization_id=$1 AND id=$3`, f.org, providerID, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE payment_provider_events SET next_attempt_at=NOW() WHERE provider_event_id=$1`, event.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.ProcessEvents(ctx); err != nil {
		t.Fatal(err)
	}
	assertPaymentOrderReservation(t, pool, f.org, created.ID, ord.ID, f.inventory, "succeeded", "paid", "committed")
}

func assertTrustedSuccessState(t *testing.T, pool *pgxpool.Pool, org uuid.UUID, paymentID, orderID, inventoryID uuid.UUID, beforeOnHand int64) {
	t.Helper()
	assertPaymentOrderReservation(t, pool, org, paymentID, orderID, inventoryID, "succeeded", "paid", "committed")
	var paidAt *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT paid_at FROM orders WHERE organization_id=$1 AND id=$2`, org, orderID).Scan(&paidAt); err != nil {
		t.Fatal(err)
	}
	if paidAt == nil {
		t.Fatal("paid_at is null")
	}
	var onHand int64
	if err := pool.QueryRow(context.Background(), `SELECT on_hand_quantity FROM inventory_levels WHERE organization_id=$1 AND id=$2`, org, inventoryID).Scan(&onHand); err != nil {
		t.Fatal(err)
	}
	if onHand != beforeOnHand {
		t.Fatalf("on hand changed from %d to %d", beforeOnHand, onHand)
	}
	var reserved, available int64
	if err := pool.QueryRow(context.Background(), `SELECT COALESCE(SUM(r.quantity),0), i.on_hand_quantity-COALESCE(SUM(r.quantity),0) FROM inventory_levels i LEFT JOIN inventory_reservations r ON r.organization_id=i.organization_id AND r.inventory_level_id=i.id AND ((r.status='active' AND r.expires_at>NOW()) OR r.status IN ('payment_held','committed')) WHERE i.organization_id=$1 AND i.id=$2 GROUP BY i.on_hand_quantity`, org, inventoryID).Scan(&reserved, &available); err != nil {
		t.Fatal(err)
	}
	if reserved != 1 || available != beforeOnHand-1 {
		t.Fatalf("reserved=%d available=%d", reserved, available)
	}
}

func assertPaymentOrderReservation(t *testing.T, pool *pgxpool.Pool, org uuid.UUID, paymentID, orderID, inventoryID uuid.UUID, paymentStatus, orderStatus, reservationStatus string) {
	t.Helper()
	var actual string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM payments WHERE organization_id=$1 AND id=$2`, org, paymentID).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != paymentStatus {
		t.Fatalf("payment status=%s", actual)
	}
	if err := pool.QueryRow(context.Background(), `SELECT status FROM orders WHERE organization_id=$1 AND id=$2`, org, orderID).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != orderStatus {
		t.Fatalf("order status=%s", actual)
	}
	if err := pool.QueryRow(context.Background(), `SELECT status FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2`, org, inventoryID).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != reservationStatus {
		t.Fatalf("reservation status=%s", actual)
	}
}
