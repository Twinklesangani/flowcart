package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func dashboardPool(t *testing.T) *pgxpool.Pool {
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

type dashboardFixture struct{ org, user, productA, productB, productC, warehouseA, warehouseB, inventoryA, inventoryB, inventoryC uuid.UUID }

func newDashboardFixture(t *testing.T, pool *pgxpool.Pool) dashboardFixture {
	t.Helper()
	f := dashboardFixture{org: uuid.New(), user: uuid.New(), productA: uuid.New(), productB: uuid.New(), productC: uuid.New(), warehouseA: uuid.New(), warehouseB: uuid.New(), inventoryA: uuid.New(), inventoryB: uuid.New(), inventoryC: uuid.New()}
	ctx := context.Background()
	for _, query := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO organizations(id,name,slug) VALUES($1,'Dashboard Test',$2)`, []any{f.org, "dashboard-" + f.org.String()}},
		{`INSERT INTO users(id,email,password_hash) VALUES($1,$2,'test-hash')`, []any{f.user, "dashboard-" + f.user.String() + "@example.com"}},
		{`INSERT INTO organization_members(organization_id,user_id,role) VALUES($1,$2,'owner')`, []any{f.org, f.user}},
		{`INSERT INTO products(id,organization_id,sku,name,unit_price_minor,currency_code) VALUES($1,$2,$3,'A',1000,'AUD'),($4,$2,$5,'B',1000,'AUD'),($6,$2,$7,'C',1000,'AUD')`, []any{f.productA, f.org, "DASH-A", f.productB, "DASH-B", f.productC, "DASH-C"}},
		{`INSERT INTO warehouses(id,organization_id,code,name) VALUES($1,$2,'A','A'),($3,$2,'B','B')`, []any{f.warehouseA, f.org, f.warehouseB}},
		{`INSERT INTO inventory_levels(id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES($1,$2,$3,$4,5,10,20),($5,$2,$6,$7,0,5,10),($8,$2,$9,$4,20,5,10)`, []any{f.inventoryA, f.org, f.productA, f.warehouseA, f.inventoryB, f.productB, f.warehouseB, f.inventoryC, f.productC}},
	} {
		if _, err := pool.Exec(ctx, query.sql, query.args...); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < 101; index++ {
		status := "pending"
		if index == 100 {
			status = "paid"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO orders(organization_id,created_by_user_id,status,idempotency_key,request_hash) VALUES($1,$2,$3,$4,$5)`, f.org, f.user, status, "dashboard-order-"+uuid.NewString(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < 2; index++ {
		if _, err := pool.Exec(ctx, `INSERT INTO inventory_transfers(organization_id,source_warehouse_id,destination_warehouse_id,created_by_user_id,creation_idempotency_key,creation_request_hash) VALUES($1,$2,$3,$4,$5,$6)`, f.org, f.warehouseA, f.warehouseB, f.user, "dashboard-transfer-"+uuid.NewString(), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, f.org)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, f.user)
	})
	return f
}

func TestMetricsAreAggregateAndTenantScoped(t *testing.T) {
	pool := dashboardPool(t)
	f := newDashboardFixture(t, pool)
	metrics, err := NewRepository(pool).Metrics(context.Background(), f.org)
	if err != nil {
		t.Fatal(err)
	}
	if metrics.OrderCount != 101 || metrics.OpenOrderCount != 100 || metrics.PaidOrderCount != 1 {
		t.Fatalf("order metrics=%+v", metrics)
	}
	if metrics.InventoryCount != 3 || metrics.LowStockCount != 1 || metrics.OutOfStockCount != 1 || metrics.WarehouseCount != 2 || metrics.TransferCount != 2 {
		t.Fatalf("domain metrics=%+v", metrics)
	}
	other, err := NewRepository(pool).Metrics(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if other.OrderCount != 0 || other.OpenOrderCount != 0 || other.PaidOrderCount != 0 || other.InventoryCount != 0 || other.WarehouseCount != 0 || other.LowStockCount != 0 || other.OutOfStockCount != 0 || other.TransferCount != 0 || len(other.Warehouses) != 0 {
		t.Fatalf("foreign metrics=%+v", other)
	}
}

func TestMetricsHandlerRequiresTenantContext(t *testing.T) {
	handler := NewHandler(NewRepository(nil))
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	response := httptest.NewRecorder()
	handler.Metrics(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestWarehouseMetricsIncludeAllInventoryLevels(t *testing.T) {
	pool := dashboardPool(t)
	f := newDashboardFixture(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO products(id,organization_id,sku,name,unit_price_minor,currency_code) SELECT gen_random_uuid(),$1,'DASH-MANY-'||g,'Many',1000,'AUD' FROM generate_series(1,101) g`, f.org); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels(organization_id,product_id,warehouse_id,on_hand_quantity) SELECT $1,p.id,$2,7 FROM products p WHERE p.organization_id=$1 AND p.sku LIKE 'DASH-MANY-%'`, f.org, f.warehouseA); err != nil {
		t.Fatal(err)
	}
	metrics, err := NewRepository(pool).Metrics(ctx, f.org)
	if err != nil {
		t.Fatal(err)
	}
	for _, warehouse := range metrics.Warehouses {
		if warehouse.ID == f.warehouseA {
			if warehouse.InventoryCount != 103 || warehouse.AvailableQuantity != 732 {
				t.Fatalf("warehouse aggregate=%+v", warehouse)
			}
			return
		}
	}
	t.Fatal("warehouse aggregate missing")
}
