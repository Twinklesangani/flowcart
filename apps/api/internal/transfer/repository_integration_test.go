package transfer

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"flowcart/apps/api/internal/fulfillment"
	"flowcart/apps/api/internal/inventory"
	"flowcart/apps/api/internal/order"
	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/pagination"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type transferFixture struct {
	org, user, productA, productB, warehouseA, warehouseB, inventoryA, inventoryB uuid.UUID
}

func transferPool(t *testing.T) *pgxpool.Pool {
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

func newTransferFixture(t *testing.T, pool *pgxpool.Pool, sourceQuantity, destinationQuantity int64, twoProducts bool) transferFixture {
	t.Helper()
	ctx := context.Background()
	f := transferFixture{org: uuid.New(), user: uuid.New(), productA: uuid.New(), warehouseA: uuid.New(), warehouseB: uuid.New(), inventoryA: uuid.New(), inventoryB: uuid.New()}
	if twoProducts {
		f.productB = uuid.New()
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO organizations (id,name,slug) VALUES ($1,$2,$3)`, []any{f.org, "Transfer Test", "transfer-" + f.org.String()}},
		{`INSERT INTO users (id,email,password_hash) VALUES ($1,$2,'test-hash')`, []any{f.user, "transfer-" + f.user.String() + "@example.com"}},
		{`INSERT INTO organization_members (organization_id,user_id,role) VALUES ($1,$2,'owner')`, []any{f.org, f.user}},
		{`INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,$4,1000,'AUD')`, []any{f.productA, f.org, "TR-A-" + f.productA.String(), "Transfer Product A"}},
		{`INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,'TR-A','Transfer A')`, []any{f.warehouseA, f.org}},
		{`INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,'TR-B','Transfer B')`, []any{f.warehouseB, f.org}},
		{`INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,$5)`, []any{f.inventoryA, f.org, f.productA, f.warehouseA, sourceQuantity}},
		{`INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4,$5)`, []any{f.inventoryB, f.org, f.productA, f.warehouseB, destinationQuantity}},
	}
	if twoProducts {
		statements = append(statements,
			struct {
				query string
				args  []any
			}{`INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,$4,1000,'AUD')`, []any{f.productB, f.org, "TR-B-" + f.productB.String(), "Transfer Product B"}},
			struct {
				query string
				args  []any
			}{`INSERT INTO inventory_levels (organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,$4)`, []any{f.org, f.productB, f.warehouseA, sourceQuantity}},
		)
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { cleanupTransferFixture(t, pool, f) })
	return f
}

func cleanupTransferFixture(t *testing.T, pool *pgxpool.Pool, f transferFixture) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Errorf("begin transfer cleanup: %v", err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `ALTER TABLE audit_events DISABLE TRIGGER audit_events_append_only_trg`); err != nil {
		t.Errorf("disable audit append-only trigger during transfer cleanup: %v", err)
		return
	}
	for _, query := range []string{
		`DELETE FROM audit_events WHERE organization_id=$1`,
		`DELETE FROM inventory_movements WHERE organization_id=$1`,
		`DELETE FROM fulfillment_items WHERE organization_id=$1`,
		`DELETE FROM fulfillments WHERE organization_id=$1`,
		`DELETE FROM inventory_transfer_items WHERE organization_id=$1`,
		`DELETE FROM inventory_transfers WHERE organization_id=$1`,
		`DELETE FROM payment_provider_events WHERE organization_id=$1`,
		`DELETE FROM payments WHERE organization_id=$1`,
		`DELETE FROM inventory_reservations WHERE organization_id=$1`,
		`DELETE FROM order_items WHERE organization_id=$1`,
		`DELETE FROM orders WHERE organization_id=$1`,
		`DELETE FROM inventory_levels WHERE organization_id=$1`,
		`DELETE FROM products WHERE organization_id=$1`,
		`DELETE FROM warehouses WHERE organization_id=$1`,
		`DELETE FROM organization_members WHERE organization_id=$1`,
		`DELETE FROM organizations WHERE id=$1`,
		`DELETE FROM users WHERE id=$1`,
	} {
		if _, err := tx.Exec(ctx, query, f.org); err != nil {
			t.Errorf("transfer cleanup: %v", err)
			return
		}
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE audit_events ENABLE TRIGGER audit_events_append_only_trg`); err != nil {
		t.Errorf("re-enable audit append-only trigger after transfer cleanup: %v", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		t.Errorf("commit transfer cleanup: %v", err)
	}
}

func TestListPageLoadsChildrenOnlyForRetainedTransfers(t *testing.T) {
	pool := transferPool(t)
	f := newTransferFixture(t, pool, 100, 0, true)
	repository := NewRepository(pool)
	ctx := context.Background()
	first, err := repository.Create(ctx, f.org, f.user, "lookahead-first", strings.Repeat("a", 64), CreateInput{SourceWarehouseID: f.warehouseA, DestinationWarehouseID: f.warehouseB, Items: []TransferItemInput{{ProductID: f.productA, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.Create(ctx, f.org, f.user, "lookahead-second", strings.Repeat("b", 64), CreateInput{SourceWarehouseID: f.warehouseA, DestinationWarehouseID: f.warehouseB, Items: []TransferItemInput{{ProductID: f.productB, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := repository.ListPage(ctx, f.org, 1, pagination.Cursor{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || len(page.Transfers) != 1 || page.Transfers[0].ID != second.ID || len(page.Transfers[0].Items) != 1 || page.Transfers[0].Items[0].ProductID != f.productB {
		t.Fatalf("page=%+v first=%s second=%s", page, first.ID, second.ID)
	}
}

func transferTenant(f transferFixture) organization.TenantContext {
	return organization.TenantContext{OrganizationID: f.org, UserID: f.user, Role: organization.RoleOwner}
}
func transferInput(f transferFixture, quantity int64) CreateInput {
	return CreateInput{SourceWarehouseID: f.warehouseA, DestinationWarehouseID: f.warehouseB, Items: []TransferItemInput{{ProductID: f.productA, Quantity: quantity}}}
}

func TestTransferConservesStockAndRecordsLedger(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 20, 5, false)
	repo := NewRepository(pool)
	created, err := NewService(repo).Create(ctx, transferTenant(f), "create-conserve", transferInput(f, 8))
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != StatusPending || created.Items[0].DestinationInventoryLevelID == uuid.Nil {
		t.Fatalf("created transfer = %+v", created)
	}
	dispatched, err := repo.Dispatch(ctx, f.org, f.user, created.ID, "dispatch-conserve")
	if err != nil {
		t.Fatal(err)
	}
	if dispatched.Status != StatusInTransit {
		t.Fatalf("dispatch status = %s", dispatched.Status)
	}
	var source, destination, inTransit int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, f.inventoryA).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, created.Items[0].DestinationInventoryLevelID).Scan(&destination); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(i.quantity),0) FROM inventory_transfer_items i JOIN inventory_transfers tr ON tr.organization_id=i.organization_id AND tr.id=i.transfer_id WHERE tr.organization_id=$1 AND tr.status='in_transit'`, f.org).Scan(&inTransit); err != nil {
		t.Fatal(err)
	}
	if source != 12 || destination != 5 || inTransit != 8 {
		t.Fatalf("after dispatch source=%d destination=%d in_transit=%d", source, destination, inTransit)
	}
	completed, err := repo.Receive(ctx, f.org, f.user, created.ID, "receive-conserve")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusCompleted {
		t.Fatalf("receive status = %s", completed.Status)
	}
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, created.Items[0].DestinationInventoryLevelID).Scan(&destination); err != nil {
		t.Fatal(err)
	}
	if destination != 13 {
		t.Fatalf("destination after receive=%d want 13", destination)
	}
	var outCount, inCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE movement_type='transfer_out'), COUNT(*) FILTER (WHERE movement_type='transfer_in') FROM inventory_movements WHERE organization_id=$1 AND transfer_id=$2`, f.org, created.ID).Scan(&outCount, &inCount); err != nil {
		t.Fatal(err)
	}
	if outCount != 1 || inCount != 1 {
		t.Fatalf("movement counts out=%d in=%d", outCount, inCount)
	}
}

func TestTransferCreationIdempotencyAndCancellation(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 10, 0, false)
	service := NewService(NewRepository(pool))
	tenant := transferTenant(f)
	input := transferInput(f, 3)
	first, err := service.Create(ctx, tenant, "create-idempotent", input)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Create(ctx, tenant, "create-idempotent", input)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay ID=%s first ID=%s", replay.ID, first.ID)
	}
	changed := input
	changed.Items = []TransferItemInput{{ProductID: f.productA, Quantity: 4}}
	if _, err := service.Create(ctx, tenant, "create-idempotent", changed); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Fatalf("different request error=%v", err)
	}
	cancelled, err := service.Cancel(ctx, tenant, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != StatusCancelled {
		t.Fatalf("cancelled status=%s", cancelled.Status)
	}
	var source int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, f.inventoryA).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != 10 {
		t.Fatalf("cancel changed source=%d", source)
	}
	var movements int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE organization_id=$1 AND transfer_id=$2`, f.org, first.ID).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if movements != 0 {
		t.Fatalf("cancel movements=%d", movements)
	}
}

func TestTransferInsufficientReservedStockRollsBack(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 10, 0, false)
	inventoryRepo := inventory.NewRepository(pool)
	repo := NewRepository(pool)
	if _, err := inventoryRepo.Reserve(ctx, f.org, f.inventoryA, 7); err != nil {
		t.Fatal(err)
	}
	created, err := repo.Create(ctx, f.org, f.user, "create-reserved", strings.Repeat("r", 64), transferInput(f, 5))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Dispatch(ctx, f.org, f.user, created.ID, "dispatch-reserved"); !errors.Is(err, ErrInsufficientTransferableStock) {
		t.Fatalf("dispatch error=%v", err)
	}
	var source int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, f.inventoryA).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != 10 {
		t.Fatalf("source=%d want 10", source)
	}
	var movements int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE organization_id=$1 AND transfer_id=$2`, f.org, created.ID).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if movements != 0 {
		t.Fatalf("movements=%d want 0", movements)
	}
}

func TestTransferDuplicateDispatchAndReceiveAreIdempotent(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 20, 0, false)
	repo := NewRepository(pool)
	created, err := repo.Create(ctx, f.org, f.user, "create-duplicate", strings.Repeat("d", 64), transferInput(f, 8))
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.Dispatch(ctx, f.org, f.user, created.ID, "dispatch-same")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Dispatch(ctx, f.org, f.user, created.ID, "dispatch-same")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatal("dispatch replay changed transfer")
	}
	if _, err := repo.Receive(ctx, f.org, f.user, created.ID, "receive-same"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Receive(ctx, f.org, f.user, created.ID, "receive-same"); err != nil {
		t.Fatal(err)
	}
	var source, destination int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, f.inventoryA).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, first.Items[0].DestinationInventoryLevelID).Scan(&destination); err != nil {
		t.Fatal(err)
	}
	if source != 12 || destination != 8 {
		t.Fatalf("source=%d destination=%d", source, destination)
	}
	var movements int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE organization_id=$1 AND transfer_id=$2`, f.org, created.ID).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if movements != 2 {
		t.Fatalf("movements=%d want 2", movements)
	}
}

func TestTransferConcurrentDuplicateDispatchAndReceive(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 20, 0, false)
	repo := NewRepository(pool)
	created, err := repo.Create(ctx, f.org, f.user, "create-concurrent-duplicate", strings.Repeat("c", 64), transferInput(f, 8))
	if err != nil {
		t.Fatal(err)
	}
	dispatchResults := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := repo.Dispatch(ctx, f.org, f.user, created.ID, "dispatch-concurrent")
			dispatchResults <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-dispatchResults; err != nil {
			t.Fatal(err)
		}
	}
	receiveResults := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := repo.Receive(ctx, f.org, f.user, created.ID, "receive-concurrent")
			receiveResults <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-receiveResults; err != nil {
			t.Fatal(err)
		}
	}
	var source, destination int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, f.inventoryA).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, created.Items[0].DestinationInventoryLevelID).Scan(&destination); err != nil {
		t.Fatal(err)
	}
	if source != 12 || destination != 8 {
		t.Fatalf("source=%d destination=%d", source, destination)
	}
	var outCount, inCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE movement_type='transfer_out'), COUNT(*) FILTER (WHERE movement_type='transfer_in') FROM inventory_movements WHERE organization_id=$1 AND transfer_id=$2`, f.org, created.ID).Scan(&outCount, &inCount); err != nil {
		t.Fatal(err)
	}
	if outCount != 1 || inCount != 1 {
		t.Fatalf("movement counts out=%d in=%d", outCount, inCount)
	}
}

func TestTransferVsAutomaticAllocationRace(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 10, 0, false)
	transferRepo := NewRepository(pool)
	orderRepo := order.NewRepository(pool)
	created, err := transferRepo.Create(ctx, f.org, f.user, "create-auto-race", strings.Repeat("z", 64), transferInput(f, 7))
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan struct {
		transferErr error
		orderErr    error
	}, 2)
	start := make(chan struct{})
	go func() {
		<-start
		_, err := transferRepo.Dispatch(ctx, f.org, f.user, created.ID, "dispatch-auto-race")
		results <- struct {
			transferErr error
			orderErr    error
		}{transferErr: err}
	}()
	go func() {
		<-start
		_, err := orderRepo.AutoCreate(ctx, f.org, f.user, "order-auto-race", strings.Repeat("o", 64), order.AutoCreateInput{Items: []order.AutoItemInput{{ProductID: f.productA, Quantity: 7}}})
		results <- struct {
			transferErr error
			orderErr    error
		}{orderErr: err}
	}()
	close(start)
	var transferErr, orderErr error
	for i := 0; i < 2; i++ {
		result := <-results
		if result.transferErr != nil {
			transferErr = result.transferErr
		}
		if result.orderErr != nil {
			orderErr = result.orderErr
		}
	}
	if transferErr != nil && orderErr != nil {
		if !errors.Is(transferErr, ErrInsufficientTransferableStock) && !errors.Is(orderErr, order.ErrInsufficientNetworkStock) {
			t.Fatalf("both operations failed unexpectedly: transfer=%v order=%v", transferErr, orderErr)
		}
	}
	var source, reserved int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, f.inventoryA).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(quantity),0) FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2 AND status IN ('active','payment_held','committed')`, f.org, f.inventoryA).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved > source {
		t.Fatalf("reserved=%d exceeds source on_hand=%d; transferErr=%v orderErr=%v", reserved, source, transferErr, orderErr)
	}
}

func TestTransferVsManualReservationRace(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 10, 0, false)
	transferRepo := NewRepository(pool)
	inventoryRepo := inventory.NewRepository(pool)
	created, err := transferRepo.Create(ctx, f.org, f.user, "create-reservation-race", strings.Repeat("q", 64), transferInput(f, 7))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := transferRepo.Dispatch(ctx, f.org, f.user, created.ID, "dispatch-reservation-race")
		results <- err
	}()
	go func() { <-start; _, err := inventoryRepo.Reserve(ctx, f.org, f.inventoryA, 4); results <- err }()
	close(start)
	for i := 0; i < 2; i++ {
		err := <-results
		if err != nil && !errors.Is(err, ErrInsufficientTransferableStock) && !errors.Is(err, inventory.ErrInsufficientAvailableStock) {
			t.Fatalf("unexpected race error: %v", err)
		}
	}
	var source, reserved int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, f.inventoryA).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(quantity),0) FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2 AND status='active'`, f.org, f.inventoryA).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved > source {
		t.Fatalf("reserved=%d exceeds source=%d", reserved, source)
	}
}

func TestTransferVsAdjustmentRace(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 10, 0, false)
	transferRepo := NewRepository(pool)
	inventoryRepo := inventory.NewRepository(pool)
	created, err := transferRepo.Create(ctx, f.org, f.user, "create-adjustment-race", strings.Repeat("j", 64), transferInput(f, 7))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := transferRepo.Dispatch(ctx, f.org, f.user, created.ID, "dispatch-adjustment-race")
		results <- err
	}()
	go func() { <-start; _, err := inventoryRepo.Adjust(ctx, f.org, f.inventoryA, -5); results <- err }()
	close(start)
	for i := 0; i < 2; i++ {
		err := <-results
		if err != nil && !errors.Is(err, ErrInsufficientTransferableStock) && !errors.Is(err, inventory.ErrStockBelowReserved) && !errors.Is(err, inventory.ErrInsufficientStock) {
			t.Fatalf("unexpected race error: %v", err)
		}
	}
	var source, reserved int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, f.inventoryA).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(quantity),0) FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2 AND status IN ('active','payment_held','committed')`, f.org, f.inventoryA).Scan(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved > source || source < 0 {
		t.Fatalf("reserved=%d source=%d", reserved, source)
	}
	var adjustmentMovements int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE organization_id=$1 AND inventory_level_id=$2 AND movement_type='adjustment'`, f.org, f.inventoryA).Scan(&adjustmentMovements); err != nil {
		t.Fatal(err)
	}
	if adjustmentMovements > 1 {
		t.Fatalf("adjustment movements=%d", adjustmentMovements)
	}
}

func TestTransferVsCommittedFulfillmentRace(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 10, 0, false)
	transferRepo := NewRepository(pool)
	orderRepo := order.NewRepository(pool)
	orderItem, err := orderRepo.Create(ctx, f.org, f.user, "fulfillment-order", strings.Repeat("u", 64), order.CreateInput{Items: []order.ItemInput{{InventoryLevelID: f.inventoryA, Quantity: 7}}})
	if err != nil {
		t.Fatal(err)
	}
	reservationID := orderItem.Items[0].ReservationID
	if reservationID == nil {
		t.Fatal("order reservation was not created")
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='paid',paid_at=NOW() WHERE organization_id=$1 AND id=$2`, f.org, orderItem.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_reservations SET status='committed' WHERE organization_id=$1 AND id=$2`, f.org, *reservationID); err != nil {
		t.Fatal(err)
	}
	created, err := transferRepo.Create(ctx, f.org, f.user, "create-fulfillment-race", strings.Repeat("y", 64), transferInput(f, 5))
	if err != nil {
		t.Fatal(err)
	}
	fulfillmentRepo := fulfillment.NewRepository(pool)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := transferRepo.Dispatch(ctx, f.org, f.user, created.ID, "dispatch-fulfillment-race")
		results <- err
	}()
	go func() {
		<-start
		_, err := fulfillmentRepo.Complete(ctx, transferTenant(f), orderItem.ID, "fulfill-race", strings.Repeat("f", 64), []uuid.UUID{*reservationID})
		results <- err
	}()
	close(start)
	for i := 0; i < 2; i++ {
		err := <-results
		if err != nil && !errors.Is(err, ErrInsufficientTransferableStock) && !errors.Is(err, fulfillment.ErrInsufficientStock) {
			t.Fatalf("unexpected fulfillment race error: %v", err)
		}
	}
	var source int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, f.inventoryA).Scan(&source); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM inventory_reservations WHERE organization_id=$1 AND id=$2`, f.org, *reservationID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if source < 0 || (status != "consumed" && status != "committed") {
		t.Fatalf("source=%d reservation_status=%s", source, status)
	}
}

func TestTransferDestinationInventoryCreationConcurrent(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 20, 0, false)
	repo := NewRepository(pool)
	results := make(chan Transfer, 2)
	errorsOut := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for i := 0; i < 2; i++ {
		go func(index int) {
			defer wait.Done()
			item, err := repo.Create(ctx, f.org, f.user, "create-concurrent-"+string(rune('a'+index)), strings.Repeat(string(rune('a'+index)), 64), transferInput(f, 2))
			results <- item
			errorsOut <- err
		}(i)
	}
	wait.Wait()
	close(results)
	close(errorsOut)
	var destinationID uuid.UUID
	for item := range results {
		if item.ID == uuid.Nil {
			t.Fatal("concurrent creation returned empty transfer")
		}
		if destinationID == uuid.Nil {
			destinationID = item.Items[0].DestinationInventoryLevelID
		} else if destinationID != item.Items[0].DestinationInventoryLevelID {
			t.Fatalf("destination IDs differ: %s and %s", destinationID, item.Items[0].DestinationInventoryLevelID)
		}
	}
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_levels WHERE organization_id=$1 AND product_id=$2 AND warehouse_id=$3`, f.org, f.productA, f.warehouseB).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("destination inventory count=%d", count)
	}
}

func TestTransferOppositeDispatchesDoNotDeadlock(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 20, 20, false)
	repo := NewRepository(pool)
	forward, err := repo.Create(ctx, f.org, f.user, "create-forward", strings.Repeat("f", 64), transferInput(f, 7))
	if err != nil {
		t.Fatal(err)
	}
	reverseInput := CreateInput{SourceWarehouseID: f.warehouseB, DestinationWarehouseID: f.warehouseA, Items: []TransferItemInput{{ProductID: f.productA, Quantity: 7}}}
	reverse, err := repo.Create(ctx, f.org, f.user, "create-reverse", strings.Repeat("v", 64), reverseInput)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { _, err := repo.Dispatch(ctx, f.org, f.user, forward.ID, "dispatch-forward"); results <- err }()
	go func() { _, err := repo.Dispatch(ctx, f.org, f.user, reverse.ID, "dispatch-reverse"); results <- err }()
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var source, destination int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, f.inventoryA).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, f.inventoryB).Scan(&destination); err != nil {
		t.Fatal(err)
	}
	if source != 13 || destination != 13 {
		t.Fatalf("opposite dispatch source=%d destination=%d", source, destination)
	}
	var inTransit int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(quantity),0) FROM inventory_transfer_items i JOIN inventory_transfers t ON t.organization_id=i.organization_id AND t.id=i.transfer_id WHERE t.organization_id=$1 AND t.status='in_transit'`, f.org).Scan(&inTransit); err != nil {
		t.Fatal(err)
	}
	if inTransit != 14 {
		t.Fatalf("in_transit=%d want 14", inTransit)
	}
}

func TestTransferCrossTenantNotFound(t *testing.T) {
	pool := transferPool(t)
	ctx := context.Background()
	f := newTransferFixture(t, pool, 10, 0, false)
	other := newTransferFixture(t, pool, 10, 0, false)
	repo := NewRepository(pool)
	input := CreateInput{SourceWarehouseID: f.warehouseA, DestinationWarehouseID: other.warehouseB, Items: []TransferItemInput{{ProductID: f.productA, Quantity: 1}}}
	if _, err := repo.Create(ctx, f.org, f.user, "cross-tenant", strings.Repeat("x", 64), input); err == nil {
		t.Fatal("cross-tenant transfer unexpectedly succeeded")
	}
	created, err := repo.Create(ctx, f.org, f.user, "private", strings.Repeat("p", 64), transferInput(f, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, other.org, created.ID); !errors.Is(err, ErrNotFound) && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-tenant get error=%v", err)
	}
}
