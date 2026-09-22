package inventory

import (
	"context"
	"errors"
	"sort"
	"testing"

	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/transfer"

	"github.com/google/uuid"
)

func TestM16LowStockRecommendationIsAdvisory(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	orgID := uuid.New()
	productID := uuid.New()
	warehouseA, warehouseB, warehouseC := uuid.New(), uuid.New(), uuid.New()
	inventoryA, inventoryB, inventoryC := uuid.New(), uuid.New(), uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO organizations (id,name,slug) VALUES ($1,'M16 Org',$2)`, orgID, "m16-"+orgID.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,'M16 Product',1000,'AUD')`, []any{productID, orgID, "M16-" + productID.String()}},
		{`INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,'A','M16 A')`, []any{warehouseA, orgID}},
		{`INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,'B','M16 B')`, []any{warehouseB, orgID}},
		{`INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,'C','M16 C')`, []any{warehouseC, orgID}},
		{`INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES ($1,$2,$3,$4,3,5,15)`, []any{inventoryA, orgID, productID, warehouseA}},
		{`INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES ($1,$2,$3,$4,20,8,15)`, []any{inventoryB, orgID, productID, warehouseB}},
		{`INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES ($1,$2,$3,$4,17,7,15)`, []any{inventoryC, orgID, productID, warehouseC}},
	} {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID) })
	repository := NewRepository(pool)
	service := NewService(repository)
	tenant := organization.TenantContext{OrganizationID: orgID, Role: organization.RoleOwner}
	before := struct {
		onHand                             int64
		transfers, reservations, movements int
	}{}
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, inventoryA).Scan(&before.onHand); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_transfers WHERE organization_id=$1`, orgID).Scan(&before.transfers); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE organization_id=$1`, orgID).Scan(&before.reservations); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_movements WHERE organization_id=$1`, orgID).Scan(&before.movements); err != nil {
		t.Fatal(err)
	}
	items, err := service.LowStock(ctx, tenant, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != inventoryA {
		t.Fatalf("low stock items = %+v", items)
	}
	item := items[0]
	if item.Status != StatusLow || item.AvailableQuantity != 3 || item.RecommendedQuantity != 12 || item.UnfulfilledQuantity != 0 {
		t.Fatalf("low stock item = %+v", item)
	}
	if len(item.DonorAllocations) != 1 || item.DonorAllocations[0].SourceInventoryLevelID != inventoryB || item.DonorAllocations[0].Quantity != 12 {
		t.Fatalf("donor allocations = %+v", item.DonorAllocations)
	}
	var afterOnHand int64
	if err := pool.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE id=$1`, inventoryA).Scan(&afterOnHand); err != nil {
		t.Fatal(err)
	}
	if afterOnHand != before.onHand {
		t.Fatalf("recommendation changed on_hand: %d -> %d", before.onHand, afterOnHand)
	}
	var after int
	for _, query := range []string{`SELECT COUNT(*) FROM inventory_transfers WHERE organization_id=$1`, `SELECT COUNT(*) FROM inventory_reservations WHERE organization_id=$1`, `SELECT COUNT(*) FROM inventory_movements WHERE organization_id=$1`} {
		if err := pool.QueryRow(ctx, query, orgID).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != 0 {
			t.Fatalf("recommendation created rows for query %q: %d", query, after)
		}
	}
}

func TestM16PolicyPersistenceAndTenantIsolation(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	first := newInventoryFixture(t, pool, "M16 Policy")
	second := newInventoryFixture(t, pool, "M16 Other")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id IN ($1,$2)`, first.organizationID, second.organizationID)
	})
	repository := NewRepository(pool)
	reorder, target := int64(5), int64(15)
	updated, err := repository.UpdateReplenishmentPolicy(ctx, first.organizationID, uuid.New(), ReplenishmentPolicyInput{ReorderPoint: &reorder, TargetStockLevel: &target})
	if !errors.Is(err, ErrNotFound) || updated.ID != uuid.Nil {
		t.Fatalf("foreign policy update = %+v, err=%v", updated, err)
	}
	created, err := repository.Create(ctx, first.organizationID, CreateInput{ProductID: first.productID, WarehouseID: first.warehouseID})
	if err != nil {
		t.Fatal(err)
	}
	updated, err = repository.UpdateReplenishmentPolicy(ctx, first.organizationID, created.ID, ReplenishmentPolicyInput{ReorderPoint: &reorder, TargetStockLevel: &target})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ReorderPoint == nil || *updated.ReorderPoint != 5 || updated.TargetStockLevel == nil || *updated.TargetStockLevel != 15 {
		t.Fatalf("updated policy = %+v", updated)
	}
	disabled, err := repository.UpdateReplenishmentPolicy(ctx, first.organizationID, created.ID, ReplenishmentPolicyInput{})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.ReorderPoint != nil || disabled.TargetStockLevel != nil {
		t.Fatalf("disabled policy = %+v", disabled)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_levels SET reorder_point=10,target_stock_level=5 WHERE id=$1`, created.ID); err == nil {
		t.Fatal("invalid policy constraint accepted")
	}
}

func TestM16AvailabilityStatusesAndReservationSemantics(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	fixture := newInventoryFixture(t, pool, "M16 Availability")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, fixture.organizationID) })
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES ($1,$2,$3,10,5,15)`, fixture.organizationID, fixture.productID, fixture.warehouseID); err != nil {
		t.Fatal(err)
	}
	var inventoryID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM inventory_levels WHERE organization_id=$1`, fixture.organizationID).Scan(&inventoryID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"active", "payment_held", "committed", "consumed"} {
		if _, err := pool.Exec(ctx, `INSERT INTO inventory_reservations (organization_id,inventory_level_id,quantity,status,expires_at) VALUES ($1,$2,2,$3,NOW()+INTERVAL '1 hour')`, fixture.organizationID, inventoryID, status); err != nil {
			t.Fatal(err)
		}
	}
	items, err := NewService(NewRepository(pool)).LowStock(ctx, organization.TenantContext{OrganizationID: fixture.organizationID}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != StatusLow || items[0].AvailableQuantity != 4 {
		t.Fatalf("reservation availability = %+v", items)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_levels SET on_hand_quantity=0 WHERE id=$1`, inventoryID); err != nil {
		t.Fatal(err)
	}
	items, err = NewService(NewRepository(pool)).LowStock(ctx, organization.TenantContext{OrganizationID: fixture.organizationID}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != StatusOutOfStock {
		t.Fatalf("out of stock = %+v", items)
	}
}

func TestM16RecommendationDoesNotBypassM15Revalidation(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	orgID, productID, sourceWarehouse, destinationWarehouse := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	sourceInventory, destinationInventory := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id,name,slug) VALUES ($1,'M16 Revalidation',$2)`, orgID, "m16-revalidation-"+orgID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$2,$3,'Product',1000,'AUD')`, productID, orgID, "REV-"+productID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,'SOURCE','Source'),($3,$2,'DEST','Destination')`, sourceWarehouse, orgID, destinationWarehouse); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES ($1,$2,$3,$4,20,8,15),($5,$2,$3,$6,0,5,15)`, sourceInventory, orgID, productID, sourceWarehouse, destinationInventory, destinationWarehouse); err != nil {
		t.Fatal(err)
	}
	userID := uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID)
	})
	if _, err := pool.Exec(ctx, `INSERT INTO users (id,email,password_hash) VALUES ($1,$2,'test-hash')`, userID, "m16-"+userID.String()+"@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organization_members (organization_id,user_id,role) VALUES ($1,$2,'owner')`, orgID, userID); err != nil {
		t.Fatal(err)
	}
	tenant := organization.TenantContext{OrganizationID: orgID, UserID: userID, Role: organization.RoleOwner}
	items, err := NewService(NewRepository(pool)).LowStock(ctx, tenant, 50)
	if err != nil || len(items) != 1 || items[0].RecommendedQuantity != 12 {
		t.Fatalf("recommendation = %+v, err=%v", items, err)
	}
	trRepo := transfer.NewRepository(pool)
	created, err := trRepo.Create(ctx, orgID, tenant.UserID, "m16-transfer", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", transfer.CreateInput{SourceWarehouseID: sourceWarehouse, DestinationWarehouseID: destinationWarehouse, Items: []transfer.TransferItemInput{{ProductID: productID, Quantity: 12}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_levels SET on_hand_quantity=8 WHERE id=$1`, sourceInventory); err != nil {
		t.Fatal(err)
	}
	if _, err := trRepo.Dispatch(ctx, orgID, tenant.UserID, created.ID, "m16-dispatch"); !errors.Is(err, transfer.ErrInsufficientTransferableStock) {
		t.Fatalf("stale recommendation dispatch error=%v", err)
	}
}

func TestLowStockPageTraversesAllRows(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	orgID, warehouseID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO organizations(id,name,slug) VALUES($1,'Low Stock Pages',$2)`, orgID, "low-stock-pages-"+orgID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses(id,organization_id,code,name) VALUES($1,$2,'PAGE','Page Warehouse')`, warehouseID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO products(id,organization_id,sku,name,unit_price_minor,currency_code) SELECT gen_random_uuid(),$1,'PAGE-'||g,'Paged Product',1000,'AUD' FROM generate_series(1,101) g`, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels(organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) SELECT $1,id,$2,0,1,10 FROM products WHERE organization_id=$1 AND sku LIKE 'PAGE-%'`, orgID, warehouseID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID) })
	service := NewService(NewRepository(pool))
	seen := make(map[uuid.UUID]bool)
	cursor := ""
	for {
		page, err := service.LowStockPage(ctx, organization.TenantContext{OrganizationID: orgID}, 50, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Inventory {
			if seen[item.ID] {
				t.Fatalf("duplicate low-stock item %s", item.ID)
			}
			seen[item.ID] = true
		}
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 101 {
		t.Fatalf("traversed %d low-stock rows, want 101", len(seen))
	}
}

func TestM16DonorTruncationPerTarget(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	orgID := uuid.New()
	target1Product, target2Product := uuid.New(), uuid.New()
	target1Warehouse, target2Warehouse := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id,name,slug) VALUES ($1,'Donor Trunc',$2)`, orgID, "donor-trunc-"+orgID.String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID) })

	if _, err := pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES 
		($1,$3,'TRUNC1','P1',1000,'AUD'), 
		($2,$3,'TRUNC2','P2',1000,'AUD')`, target1Product, target2Product, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES 
		($1,$3,'T1','W1'), 
		($2,$3,'T2','W2')`, target1Warehouse, target2Warehouse, orgID); err != nil {
		t.Fatal(err)
	}

	target1Inventory, target2Inventory := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES 
		($1,$3,$4,$5,0,5,10), 
		($2,$3,$6,$7,0,5,10)`, target1Inventory, target2Inventory, orgID, target1Product, target1Warehouse, target2Product, target2Warehouse); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 7; i++ {
		wID := uuid.New()
		if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, wID, orgID, "D1"+string(rune('A'+i)), "Donor "+string(rune('A'+i))); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES ($1,$2,$3,$4,20,5,20)`, uuid.New(), orgID, target1Product, wID); err != nil {
			t.Fatal(err)
		}
	}

	nonQualifyingW := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,'NQ','NonQualifying')`, nonQualifyingW, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES ($1,$2,$3,$4,5,5,10)`, uuid.New(), orgID, target1Product, nonQualifyingW); err != nil {
		t.Fatal(err)
	}

	service := NewService(NewRepository(pool))
	items, err := service.LowStock(ctx, organization.TenantContext{OrganizationID: orgID}, 50)
	if err != nil {
		t.Fatal(err)
	}

	var t1, t2 *LowStockItem
	for i := range items {
		if items[i].ID == target1Inventory {
			t1 = &items[i]
		} else if items[i].ID == target2Inventory {
			t2 = &items[i]
		}
	}

	if t1 == nil || t2 == nil {
		t.Fatal("missing targets")
	}

	if !t1.DonorsTruncated {
		t.Error("target1 should be truncated (has 7 qualifying)")
	}
	if t2.DonorsTruncated {
		t.Error("target2 should not be truncated (has 0 donors)")
	}
}

func TestM16DonorAllocationsAreBoundedPerTargetAndPreselectionIs32(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	orgID := uuid.New()
	productOne, productTwo, boundaryProduct := uuid.New(), uuid.New(), uuid.New()
	targetWarehouseOne, targetWarehouseTwo, boundaryTargetWarehouse := uuid.New(), uuid.New(), uuid.New()
	targetOne, targetTwo, boundaryTarget := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id,name,slug) VALUES ($1,'Donor Allocation Bounds',$2)`, orgID, "donor-allocation-bounds-"+orgID.String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, orgID) })
	if _, err := pool.Exec(ctx, `INSERT INTO products (id,organization_id,sku,name,unit_price_minor,currency_code) VALUES ($1,$4,'BOUND1','Bound 1',1000,'AUD'),($2,$4,'BOUND2','Bound 2',1000,'AUD'),($3,$4,'BOUND3','Bound 3',1000,'AUD')`, productOne, productTwo, boundaryProduct, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$4,'T1','Target 1'),($2,$4,'T2','Target 2'),($3,$4,'TB','Boundary Target')`, targetWarehouseOne, targetWarehouseTwo, boundaryTargetWarehouse, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES ($1,$2,$3,$4,0,5,200),($5,$2,$6,$7,0,5,200),($8,$2,$9,$10,0,5,45)`, targetOne, orgID, productOne, targetWarehouseOne, targetTwo, productTwo, targetWarehouseTwo, boundaryTarget, boundaryProduct, boundaryTargetWarehouse); err != nil {
		t.Fatal(err)
	}
	for targetIndex, productID := range []uuid.UUID{productOne, productTwo} {
		for donorIndex := 0; donorIndex < 7; donorIndex++ {
			warehouseID := uuid.New()
			code := "D" + string(rune('A'+targetIndex*7+donorIndex))
			if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, warehouseID, orgID, code, "Donor "+code); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES ($1,$2,$3,$4,20,5,20)`, uuid.New(), orgID, productID, warehouseID); err != nil {
				t.Fatal(err)
			}
		}
	}
	zeroWarehouse := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,'ZERO','Zero Transferable')`, zeroWarehouse, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES ($1,$2,$3,$4,5,5,20)`, uuid.New(), orgID, productOne, zeroWarehouse); err != nil {
		t.Fatal(err)
	}
	boundaryWarehouses := make([]uuid.UUID, 33)
	for index := range boundaryWarehouses {
		boundaryWarehouses[index] = uuid.New()
	}
	sort.Slice(boundaryWarehouses, func(i, j int) bool { return boundaryWarehouses[i].String() < boundaryWarehouses[j].String() })
	for index, warehouseID := range boundaryWarehouses {
		code := "C" + string(rune('A'+index%26)) + string(rune('A'+index/26))
		if _, err := pool.Exec(ctx, `INSERT INTO warehouses (id,organization_id,code,name) VALUES ($1,$2,$3,$4)`, warehouseID, orgID, code, "Boundary "+code); err != nil {
			t.Fatal(err)
		}
		onHand := int64(5)
		if index == 0 || index == len(boundaryWarehouses)-1 {
			onHand = 20
		}
		if _, err := pool.Exec(ctx, `INSERT INTO inventory_levels (id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES ($1,$2,$3,$4,$5,5,20)`, uuid.New(), orgID, boundaryProduct, warehouseID, onHand); err != nil {
			t.Fatal(err)
		}
	}

	items, err := NewService(NewRepository(pool)).LowStock(ctx, organization.TenantContext{OrganizationID: orgID}, 50)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[uuid.UUID]LowStockItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	for _, targetID := range []uuid.UUID{targetOne, targetTwo} {
		item, ok := byID[targetID]
		if !ok {
			t.Fatalf("missing target %s", targetID)
		}
		if len(item.DonorAllocations) != replenishmentDonorLimit || !item.DonorsTruncated {
			t.Fatalf("target %s allocations=%d truncated=%v", targetID, len(item.DonorAllocations), item.DonorsTruncated)
		}
		if item.RecommendedQuantity != int64(replenishmentDonorLimit*15) || item.UnfulfilledQuantity != 110 {
			t.Fatalf("target %s recommendation=%d unfulfilled=%d", targetID, item.RecommendedQuantity, item.UnfulfilledQuantity)
		}
	}
	boundary, ok := byID[boundaryTarget]
	if !ok {
		t.Fatalf("missing boundary target %s", boundaryTarget)
	}
	if len(boundary.DonorAllocations) != 1 || boundary.RecommendedQuantity != 15 || boundary.UnfulfilledQuantity != 30 || boundary.DonorsTruncated {
		t.Fatalf("boundary recommendation allocations=%d recommended=%d unfulfilled=%d truncated=%v", len(boundary.DonorAllocations), boundary.RecommendedQuantity, boundary.UnfulfilledQuantity, boundary.DonorsTruncated)
	}
}
