package payment_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"flowcart/apps/api/internal/order"
	"flowcart/apps/api/internal/payment"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct{ org, user, product, warehouse, inventory uuid.UUID }

func poolForPayment(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set; PostgreSQL integration test skipped")
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
func paymentFixture(t *testing.T, pool *pgxpool.Pool, price int64) fixture {
	t.Helper()
	ctx := context.Background()
	f := fixture{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	t.Cleanup(func() {
		cleanupPaymentFixture(t, pool, f.org)
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, f.user); err != nil {
			t.Errorf("clean up payment test user: %v", err)
		}
	})
	_, err := pool.Exec(ctx, `INSERT INTO organizations (id,name,slug) VALUES ($1,$2,$3)`, f.org, "Payment Test", "payment-"+f.org.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO users (id,email,password_hash) VALUES ($1,$2,'test')`, f.user, "payment-"+f.user.String()+"@example.com")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO organization_members (organization_id,user_id,role) VALUES ($1,$2,'owner')`, f.org, f.user)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,'Payment Product',$4,'AUD')`, f.product, f.org, "PAY-"+f.product.String(), price)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,'Payment Warehouse')`, f.warehouse, f.org, "PAY-"+f.warehouse.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,10)`, f.inventory, f.org, f.product, f.warehouse)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func cleanupPaymentFixture(t *testing.T, pool *pgxpool.Pool, org uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Errorf("begin payment test cleanup: %v", err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `ALTER TABLE audit_events DISABLE TRIGGER audit_events_append_only_trg`); err != nil {
		t.Errorf("disable audit append-only trigger during payment cleanup: %v", err)
		return
	}
	for _, query := range []string{
		`DELETE FROM audit_events WHERE organization_id=$1`,
		`DELETE FROM payment_provider_events WHERE organization_id=$1`,
		`DELETE FROM payments WHERE organization_id=$1`,
		`DELETE FROM inventory_reservations WHERE organization_id=$1`,
		`DELETE FROM order_items WHERE organization_id=$1`,
		`DELETE FROM orders WHERE organization_id=$1`,
		`DELETE FROM inventory_levels WHERE organization_id=$1`,
		`DELETE FROM organizations WHERE id=$1`,
	} {
		if _, err := tx.Exec(ctx, query, org); err != nil {
			t.Errorf("clean up payment test organization: %v", err)
			return
		}
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE audit_events ENABLE TRIGGER audit_events_append_only_trg`); err != nil {
		t.Errorf("re-enable audit append-only trigger after payment cleanup: %v", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		t.Errorf("commit payment test cleanup: %v", err)
	}
}
func createPaymentOrder(t *testing.T, pool *pgxpool.Pool, f fixture, quantity int64) order.Order {
	t.Helper()
	item, err := order.NewRepository(pool).Create(context.Background(), f.org, f.user, "order-"+uuid.New().String(), strings.Repeat("d", 64), order.CreateInput{Items: []order.ItemInput{{InventoryLevelID: f.inventory, Quantity: quantity}}})
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func TestPaymentRepositorySnapshotsReplayExpiryAndAttempts(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	orderItem := createPaymentOrder(t, pool, f, 2)
	repo := payment.NewRepository(pool)
	first, err := repo.Create(ctx, f.org, f.user, orderItem.ID, "payment-key", strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	if first.AmountMinor != 4000 || first.CurrencyCode != "AUD" {
		t.Fatalf("payment snapshot=%+v", first)
	}
	replay, err := repo.Create(ctx, f.org, f.user, orderItem.ID, "payment-key", strings.Repeat("1", 64))
	if err != nil || replay.ID != first.ID {
		t.Fatalf("replay=%v %+v", err, replay)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_reservations SET expires_at=NOW()-INTERVAL '1 minute' WHERE order_item_id=$1`, orderItem.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	replay, err = repo.Create(ctx, f.org, f.user, orderItem.ID, "payment-key", strings.Repeat("1", 64))
	if err != nil || replay.ID != first.ID {
		t.Fatalf("replay after expiry=%v %+v", err, replay)
	}
	if _, err := repo.Create(ctx, f.org, f.user, orderItem.ID, "new-payment-key", strings.Repeat("2", 64)); !errors.Is(err, payment.ErrReservationExpired) {
		t.Fatalf("new after expiry=%v", err)
	}
}
func TestPaymentRepositoryAttemptsAndTenantIsolation(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 2000)
	other := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org); cleanupPaymentFixture(t, pool, other.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	repo := payment.NewRepository(pool)
	first, err := repo.Create(ctx, f.org, f.user, ord.ID, "pending-key", strings.Repeat("3", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, f.org, f.user, ord.ID, "second-key", strings.Repeat("4", 64)); !errors.Is(err, payment.ErrPaymentInProgress) {
		t.Fatalf("pending second=%v", err)
	}
	if _, err := repo.Get(ctx, other.org, first.ID); !errors.Is(err, payment.ErrPaymentNotFound) {
		t.Fatalf("foreign get=%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE payments SET status='failed' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, f.org, f.user, ord.ID, "second-key", strings.Repeat("4", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE payments SET status='succeeded' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, f.org, f.user, ord.ID, "third-key", strings.Repeat("5", 64)); !errors.Is(err, payment.ErrPaymentAlreadySucceeded) {
		t.Fatalf("succeeded second=%v", err)
	}
}
func TestPaymentRepositoryConcurrentSameKey(t *testing.T) {
	pool := poolForPayment(t)
	f := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	repo := payment.NewRepository(pool)
	results := make(chan payment.Payment, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			item, err := repo.Create(context.Background(), f.org, f.user, ord.ID, "same-key", strings.Repeat("6", 64))
			results <- item
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	var id uuid.UUID
	for item := range results {
		if id == uuid.Nil {
			id = item.ID
		} else if id != item.ID {
			t.Fatalf("different IDs %s/%s", id, item.ID)
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM payments WHERE organization_id=$1`, f.org).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("payment count=%d", count)
	}
}
func TestPaymentRepositoryZeroTotal(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 0)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	if _, err := payment.NewRepository(pool).Create(ctx, f.org, f.user, ord.ID, "zero-key", strings.Repeat("7", 64)); !errors.Is(err, payment.ErrPaymentNotRequired) {
		t.Fatalf("zero payment=%v", err)
	}
}

func TestPaymentCancellationInteraction(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	paymentRepo := payment.NewRepository(pool)
	created, err := paymentRepo.Create(ctx, f.org, f.user, ord.ID, "cancel-key", strings.Repeat("8", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := order.NewRepository(pool).Cancel(ctx, f.org, ord.ID); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM payments WHERE id=$1`, created.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != payment.StatusCancelled {
		t.Fatalf("pending payment status=%s", status)
	}
	ord2 := createPaymentOrder(t, pool, f, 1)
	succeeded, err := paymentRepo.Create(ctx, f.org, f.user, ord2.ID, "succeeded-key", strings.Repeat("9", 64))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE payments SET status='succeeded' WHERE id=$1`, succeeded.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := order.NewRepository(pool).Cancel(ctx, f.org, ord2.ID); !errors.Is(err, payment.ErrPaymentAlreadySucceeded) {
		t.Fatalf("succeeded cancellation=%v", err)
	}
}

func TestPaymentCreationCancellationRace(t *testing.T) {
	pool := poolForPayment(t)
	ctx := context.Background()
	f := paymentFixture(t, pool, 2000)
	t.Cleanup(func() { cleanupPaymentFixture(t, pool, f.org) })
	ord := createPaymentOrder(t, pool, f, 1)
	paymentRepo := payment.NewRepository(pool)
	orderRepo := order.NewRepository(pool)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := paymentRepo.Create(ctx, f.org, f.user, ord.ID, "race-payment", strings.Repeat("a", 64))
		results <- err
	}()
	go func() { defer wg.Done(); _, err := orderRepo.Cancel(ctx, f.org, ord.ID); results <- err }()
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil && !errors.Is(err, payment.ErrOrderCancelled) && !errors.Is(err, payment.ErrPaymentAlreadySucceeded) {
			t.Fatal(err)
		}
	}
	var orderStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE organization_id=$1 AND id=$2`, f.org, ord.ID).Scan(&orderStatus); err != nil {
		t.Fatal(err)
	}
	if orderStatus != order.StatusCancelled {
		t.Fatalf("order status=%s", orderStatus)
	}
	var pending int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM payments WHERE organization_id=$1 AND order_id=$2 AND status='pending'`, f.org, ord.ID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("pending payments after cancellation=%d", pending)
	}
	var active int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2 AND status='active' AND expires_at > NOW()`, f.org, f.inventory).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("active reservations after cancellation=%d", active)
	}
}

func TestPaymentRejectsReservationThatExpiresWhileWaitingForInventory(t *testing.T) {
	pool := poolForPayment(t)
	f := paymentFixture(t, pool, 2000)
	ord := createPaymentOrder(t, pool, f, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, `SELECT id FROM inventory_levels WHERE organization_id=$1 AND id=$2 FOR UPDATE`, f.org, f.inventory); err != nil {
		t.Fatal(err)
	}
	config := pool.Config()
	appName := "payment-expiry-" + uuid.NewString()
	config.ConnConfig.RuntimeParams["application_name"] = appName
	waitingPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer waitingPool.Close()
	result := make(chan error, 1)
	go func() {
		_, err := payment.NewRepository(waitingPool).Create(ctx, f.org, f.user, ord.ID, "wait-expiry", strings.Repeat("e", 64))
		result <- err
	}()
	// Observe the actual database lock wait; do not assume a goroutine has started.
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, appName).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("payment completed before lock release: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	// The payment transaction already began; make the hold expire before it gets the lock.
	if _, err := blocker.Exec(ctx, `UPDATE inventory_reservations SET expires_at=clock_timestamp() WHERE organization_id=$1 AND order_item_id=$2`, f.org, ord.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, payment.ErrReservationExpired) {
		t.Fatalf("payment after lock wait = %v, want reservation expired", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM payments WHERE organization_id=$1 AND order_id=$2`, f.org, ord.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("created %d payments against expired stock", count)
	}
}
