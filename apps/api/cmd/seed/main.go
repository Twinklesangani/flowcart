package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"flowcart/apps/api/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	demoSlug     = "flowcart-demo-retail"
	demoPassword = "FlowCart-Demo-Only-2026!"
)

var namespace = uuid.MustParse("6b9e7ee0-5f83-4d0a-a19f-f7e3cbf8c9a1")

type product struct {
	id    uuid.UUID
	sku   string
	name  string
	price int64
}

type warehouse struct {
	id   uuid.UUID
	code string
	name string
}

type seedData struct {
	org        uuid.UUID
	owner      uuid.UUID
	manager    uuid.UUID
	viewer     uuid.UUID
	warehouses []warehouse
	products   []product
	inventory  map[string]uuid.UUID
}

func stable(name string) uuid.UUID { return uuid.NewSHA1(namespace, []byte(name)) }

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "seed failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if os.Getenv("FLOWCART_SEED_CONFIRM") != "FLOWCART_DEMO" {
		return errors.New("FLOWCART_SEED_CONFIRM=FLOWCART_DEMO is required")
	}
	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if !safeSeedEnvironment(appEnv) {
		return fmt.Errorf("refusing to seed APP_ENV=%q", appEnv)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return errors.New("connect database failed; check DATABASE_URL")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	passwordHash, err := auth.HashPassword(demoPassword)
	if err != nil {
		return err
	}
	data := makeSeedData()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin seed transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := seedBase(ctx, tx, data, passwordHash); err != nil {
		return err
	}
	if err := seedInventory(ctx, tx, data); err != nil {
		return err
	}
	if err := seedOrders(ctx, tx, data); err != nil {
		return err
	}
	if err := seedTransfers(ctx, tx, data); err != nil {
		return err
	}
	if err := seedAudit(ctx, tx, data); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed transaction: %w", err)
	}
	fmt.Println("FlowCart demo seed ready")
	fmt.Printf("organization: %s (%s)\n", demoSlug, data.org)
	fmt.Println("demo credentials (DEMO ONLY - NEVER USE IN PRODUCTION):")
	fmt.Printf("  owner:   owner@flowcart.demo / %s\n", demoPassword)
	fmt.Printf("  manager: manager@flowcart.demo / %s\n", demoPassword)
	fmt.Printf("  viewer:  viewer@flowcart.demo / %s\n", demoPassword)
	return nil
}

func makeSeedData() seedData {
	products := []product{
		{stable("product:espresso-machine"), "BREW-ESP-01", "Barista Espresso Machine", 34900},
		{stable("product:coffee-beans"), "BREW-BEAN-01", "Single Origin Coffee Beans", 1800},
		{stable("product:pour-over"), "BREW-POUR-01", "Ceramic Pour Over Set", 4200},
		{stable("product:milk-frother"), "BREW-FROTH-01", "Handheld Milk Frother", 2400},
		{stable("product:tea-kettle"), "HOME-KET-01", "Electric Tea Kettle", 6900},
		{stable("product:linen-towels"), "HOME-LINEN-01", "Linen Kitchen Towel Set", 2600},
		{stable("product:desk-lamp"), "HOME-LAMP-01", "Adjustable Desk Lamp", 7900},
		{stable("product:travel-mug"), "TRVL-MUG-01", "Insulated Travel Mug", 3200},
		{stable("product:backpack"), "TRVL-PACK-01", "Everyday Commuter Backpack", 8900},
		{stable("product:yoga-mat"), "WELL-MAT-01", "Cork Yoga Mat", 5200},
		{stable("product:running-bottle"), "WELL-BTL-01", "Stainless Running Bottle", 2800},
		{stable("product:cotton-tee"), "APP-TEE-01", "Organic Cotton T-Shirt", 3500},
		{stable("product:wool-socks"), "APP-SOCK-01", "Merino Wool Socks", 2200},
		{stable("product:weekender"), "TRVL-WEEK-01", "Canvas Weekender Bag", 11900},
	}
	warehouses := []warehouse{{stable("warehouse:melbourne"), "MEL", "Melbourne"}, {stable("warehouse:sydney"), "SYD", "Sydney"}, {stable("warehouse:brisbane"), "BNE", "Brisbane"}}
	inventory := map[string]uuid.UUID{}
	for _, p := range products {
		for _, w := range warehouses {
			inventory[p.sku+":"+w.code] = stable("inventory:" + p.sku + ":" + w.code)
		}
	}
	return seedData{org: stable("organization:" + demoSlug), owner: stable("user:owner@flowcart.demo"), manager: stable("user:manager@flowcart.demo"), viewer: stable("user:viewer@flowcart.demo"), warehouses: warehouses, products: products, inventory: inventory}
}

func seedBase(ctx context.Context, tx pgx.Tx, d seedData, passwordHash string) error {
	if err := exec(ctx, tx, `INSERT INTO organizations(id,name,slug) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name WHERE organizations.slug=$3`, d.org, "FlowCart Demo Retail", demoSlug); err != nil {
		return err
	}
	users := []struct {
		id                 uuid.UUID
		email, first, last string
	}{{d.owner, "owner@flowcart.demo", "Olivia", "Owner"}, {d.manager, "manager@flowcart.demo", "Morgan", "Manager"}, {d.viewer, "viewer@flowcart.demo", "Vera", "Viewer"}}
	for _, u := range users {
		if err := exec(ctx, tx, `INSERT INTO users(id,email,first_name,last_name,password_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO UPDATE SET first_name=EXCLUDED.first_name,last_name=EXCLUDED.last_name`, u.id, u.email, u.first, u.last, passwordHash); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		id   uuid.UUID
		role string
	}{{d.owner, "owner"}, {d.manager, "warehouse_manager"}, {d.viewer, "viewer"}} {
		if err := exec(ctx, tx, `INSERT INTO organization_members(organization_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT(organization_id,user_id) DO UPDATE SET role=EXCLUDED.role`, d.org, item.id, item.role); err != nil {
			return err
		}
	}
	for _, w := range d.warehouses {
		if err := exec(ctx, tx, `INSERT INTO warehouses(id,organization_id,code,name,city,state,country_code) VALUES($1,$2,$3,$4,$5,$6,'AU') ON CONFLICT(id) DO UPDATE SET code=EXCLUDED.code,name=EXCLUDED.name,city=EXCLUDED.city,state=EXCLUDED.state`, w.id, d.org, w.code, w.name, w.name, map[string]string{"Melbourne": "VIC", "Sydney": "NSW", "Brisbane": "QLD"}[w.name]); err != nil {
			return err
		}
	}
	for _, p := range d.products {
		if err := exec(ctx, tx, `INSERT INTO products(id,organization_id,sku,name,is_active,unit_price_minor,currency_code) VALUES($1,$2,$3,$4,true,$5,'AUD') ON CONFLICT(id) DO UPDATE SET sku=EXCLUDED.sku,name=EXCLUDED.name,is_active=true,unit_price_minor=EXCLUDED.unit_price_minor,currency_code='AUD'`, p.id, d.org, p.sku, p.name, p.price); err != nil {
			return err
		}
	}
	return nil
}

func seedInventory(ctx context.Context, tx pgx.Tx, d seedData) error {
	for index, p := range d.products {
		for wi, w := range d.warehouses {
			quantity := int64(24 + index*3 + wi*4)
			var reorder, target any
			switch p.sku {
			case "BREW-BEAN-01":
				if w.code == "BNE" {
					quantity, reorder, target = 2, 5, 12
				} else {
					quantity, reorder, target = 28, 10, 30
				}
			case "BREW-POUR-01":
				if w.code == "SYD" {
					quantity, reorder, target = 1, 4, 10
				} else if w.code == "MEL" {
					quantity, reorder, target = 24, 8, 24
				} else {
					quantity, reorder, target = 18, 8, 24
				}
			case "BREW-FROTH-01":
				if w.code == "BNE" {
					quantity, reorder, target = 0, 3, 12
				}
			case "HOME-KET-01":
				if w.code == "BNE" {
					quantity, reorder, target = 3, 5, 12
				} else if w.code == "MEL" {
					quantity, reorder, target = 20, 10, 20
				} else {
					quantity, reorder, target = 16, nil, nil
				}
			}
			if p.sku == "HOME-LINEN-01" {
				reorder, target = nil, nil
			}
			id := d.inventory[p.sku+":"+w.code]
			if err := exec(ctx, tx, `INSERT INTO inventory_levels(id,organization_id,product_id,warehouse_id,on_hand_quantity,reorder_point,target_stock_level) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET on_hand_quantity=EXCLUDED.on_hand_quantity,reorder_point=EXCLUDED.reorder_point,target_stock_level=EXCLUDED.target_stock_level,updated_at=NOW()`, id, d.org, p.id, w.id, quantity, reorder, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedOrders(ctx context.Context, tx pgx.Tx, d seedData) error {
	// Pending ordinary order.
	if err := demoOrder(ctx, tx, d, "order:pending", d.owner, "pending", "manual", "", []orderLine{{"BREW-ESP-01", "MEL", 1, "active"}}, nil); err != nil {
		return err
	}
	// Automatic single-warehouse order.
	if err := demoOrder(ctx, tx, d, "order:auto-single", d.manager, "pending", "automatic", "minimize_splits_v1", []orderLine{{"HOME-LAMP-01", "SYD", 2, "active"}}, nil); err != nil {
		return err
	}
	// Automatic multi-warehouse order uses two reservations for one item.
	if err := demoSplitOrder(ctx, tx, d); err != nil {
		return err
	}
	// Paid order with committed reservation and successful payment.
	if err := demoOrder(ctx, tx, d, "order:paid", d.owner, "paid", "manual", "", []orderLine{{"WELL-MAT-01", "MEL", 1, "committed"}}, &paymentState{paid: true}); err != nil {
		return err
	}
	// Partially fulfilled order: one consumed item and one committed item.
	if err := demoOrder(ctx, tx, d, "order:partial", d.owner, "partially_fulfilled", "manual", "", []orderLine{{"WELL-BTL-01", "MEL", 1, "consumed"}, {"APP-TEE-01", "MEL", 1, "committed"}}, &paymentState{paid: true, partial: true}); err != nil {
		return err
	}
	// Fulfilled order with consumed reservation and fulfillment movement.
	if err := demoOrder(ctx, tx, d, "order:fulfilled", d.owner, "fulfilled", "manual", "", []orderLine{{"APP-SOCK-01", "SYD", 2, "consumed"}}, &paymentState{paid: true, fulfilled: true}); err != nil {
		return err
	}
	return nil
}

type orderLine struct {
	sku, warehouse    string
	quantity          int64
	reservationStatus string
}
type paymentState struct{ paid, partial, fulfilled bool }

func demoSplitOrder(ctx context.Context, tx pgx.Tx, d seedData) error {
	key := "order:auto-multi"
	id := stable(key)
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if err := exec(ctx, tx, `INSERT INTO orders(id,organization_id,created_by_user_id,status,idempotency_key,request_hash,allocation_method,allocation_strategy,currency_code,subtotal_minor,created_at,updated_at) VALUES($1,$2,$3,'pending',$4,$5,'automatic','minimize_splits_v1','AUD',$6,$7,$7) ON CONFLICT(id) DO UPDATE SET status='pending',allocation_method='automatic',allocation_strategy='minimize_splits_v1',subtotal_minor=EXCLUDED.subtotal_minor`, id, d.org, d.manager, key, hashValue("hash:"+key), int64(4*8900), now); err != nil {
		return err
	}
	if err := exec(ctx, tx, `DELETE FROM inventory_reservations WHERE organization_id=$1 AND order_item_id IN (SELECT id FROM order_items WHERE organization_id=$1 AND order_id=$2)`, d.org, id); err != nil {
		return err
	}
	if err := exec(ctx, tx, `DELETE FROM order_items WHERE organization_id=$1 AND order_id=$2`, d.org, id); err != nil {
		return err
	}
	p, ok := findProduct(d.products, "TRVL-PACK-01")
	if !ok {
		return errors.New("multi-warehouse product not found")
	}
	itemID := stable(key + ":item")
	if err := exec(ctx, tx, `INSERT INTO order_items(id,organization_id,order_id,product_id,sku_snapshot,product_name_snapshot,quantity,unit_price_minor_snapshot,currency_code_snapshot,line_total_minor) VALUES($1,$2,$3,$4,$5,$6,4,$7,'AUD',$8) ON CONFLICT(id) DO UPDATE SET quantity=4,line_total_minor=EXCLUDED.line_total_minor`, itemID, d.org, id, p.id, p.sku, p.name, p.price, p.price*4); err != nil {
		return err
	}
	for index, warehouseCode := range []string{"MEL", "SYD"} {
		reservationID := stable(fmt.Sprintf("%s:reservation:%d", key, index))
		if err := exec(ctx, tx, `INSERT INTO inventory_reservations(id,organization_id,inventory_level_id,order_item_id,quantity,status,expires_at,created_at) VALUES($1,$2,$3,$4,2,'active',$5,$6) ON CONFLICT(id) DO UPDATE SET quantity=2,status='active',expires_at=EXCLUDED.expires_at`, reservationID, d.org, d.inventory[p.sku+":"+warehouseCode], itemID, time.Now().UTC().Add(24*time.Hour), now); err != nil {
			return err
		}
	}
	return nil
}

func demoOrder(ctx context.Context, tx pgx.Tx, d seedData, key string, user uuid.UUID, status, method, strategy string, lines []orderLine, payment *paymentState) error {
	id := stable(key)
	hash := hashValue("hash:" + key)
	var subtotal int64
	for _, line := range lines {
		p, ok := findProduct(d.products, line.sku)
		if !ok {
			return fmt.Errorf("product %s not found", line.sku)
		}
		subtotal += p.price * line.quantity
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	var paidAt, fulfilledAt any
	if payment != nil && payment.paid {
		paidAt = now.Add(time.Hour)
	}
	if payment != nil && payment.fulfilled {
		fulfilledAt = now.Add(3 * time.Hour)
	}
	if err := exec(ctx, tx, `INSERT INTO orders(id,organization_id,created_by_user_id,status,idempotency_key,request_hash,allocation_method,allocation_strategy,currency_code,subtotal_minor,paid_at,fulfilled_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),'AUD',$9,$10,$11,$12,$12) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,allocation_method=EXCLUDED.allocation_method,allocation_strategy=EXCLUDED.allocation_strategy,currency_code='AUD',subtotal_minor=EXCLUDED.subtotal_minor,paid_at=EXCLUDED.paid_at,fulfilled_at=EXCLUDED.fulfilled_at`, id, d.org, user, status, key, hash, method, strategy, subtotal, paidAt, fulfilledAt, now); err != nil {
		return err
	}
	for index, line := range lines {
		p, ok := findProduct(d.products, line.sku)
		if !ok {
			return fmt.Errorf("product %s not found", line.sku)
		}
		itemID := stable(key + ":item:" + fmt.Sprint(index))
		price := p.price
		total := price * line.quantity
		if err := exec(ctx, tx, `INSERT INTO order_items(id,organization_id,order_id,product_id,sku_snapshot,product_name_snapshot,quantity,unit_price_minor_snapshot,currency_code_snapshot,line_total_minor) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'AUD',$9) ON CONFLICT(id) DO UPDATE SET quantity=EXCLUDED.quantity,line_total_minor=EXCLUDED.line_total_minor`, itemID, d.org, id, p.id, p.sku, p.name, line.quantity, price, total); err != nil {
			return err
		}
		inventoryID := d.inventory[line.sku+":"+line.warehouse]
		reservationID := stable(key + ":reservation:" + fmt.Sprint(index))
		expires := time.Now().UTC().Add(24 * time.Hour)
		if err := exec(ctx, tx, `INSERT INTO inventory_reservations(id,organization_id,inventory_level_id,order_item_id,quantity,status,expires_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET quantity=EXCLUDED.quantity,status=EXCLUDED.status,expires_at=EXCLUDED.expires_at`, reservationID, d.org, inventoryID, itemID, line.quantity, line.reservationStatus, expires, now); err != nil {
			return err
		}
		if line.reservationStatus == "consumed" {
			if err := fulfillmentRow(ctx, tx, d, key, itemID, reservationID, inventoryID, line.quantity, line.warehouse, p.id, user, now); err != nil {
				return err
			}
		}
	}
	if payment != nil && payment.paid {
		if err := paymentRow(ctx, tx, d, key, id, user, subtotal, now); err != nil {
			return err
		}
	}
	return nil
}

func paymentRow(ctx context.Context, tx pgx.Tx, d seedData, key string, orderID, user uuid.UUID, amount int64, now time.Time) error {
	pid := stable(key + ":payment")
	hash := hashValue("payment-hash:" + key)
	if err := exec(ctx, tx, `INSERT INTO payments(id,organization_id,order_id,created_by_user_id,status,amount_minor,currency_code,idempotency_key,request_hash,created_at,updated_at) VALUES($1,$2,$3,$4,'succeeded',$5,'AUD',$6,$7,$8,$8) ON CONFLICT(id) DO UPDATE SET status='succeeded',amount_minor=EXCLUDED.amount_minor`, pid, d.org, orderID, user, amount, key+":payment", hash, now); err != nil {
		return err
	}
	return nil
}

func fulfillmentRow(ctx context.Context, tx pgx.Tx, d seedData, key string, itemID, reservationID, inventoryID uuid.UUID, quantity int64, warehouseCode string, productID, user uuid.UUID, now time.Time) error {
	w := warehouseByCode(d.warehouses, warehouseCode)
	fid := stable(key + ":fulfillment")
	if err := exec(ctx, tx, `INSERT INTO fulfillments(id,organization_id,order_id,warehouse_id,created_by_user_id,status,idempotency_key,request_hash,created_at,completed_at) SELECT $1,$2,order_id,$3,$4,'completed',$5,$6,$7,$7 FROM order_items WHERE id=$8 ON CONFLICT(id) DO NOTHING`, fid, d.org, w.id, user, key+":fulfillment", hashValue("fulfillment-hash:"+key), now, itemID); err != nil {
		return err
	}
	if err := exec(ctx, tx, `INSERT INTO fulfillment_items(id,organization_id,fulfillment_id,order_id,order_item_id,reservation_id,inventory_level_id,warehouse_id,quantity) SELECT $1,$2,$3,order_id,$4,$5,$6,$7,$8 FROM order_items WHERE id=$4 ON CONFLICT(id) DO NOTHING`, stable(key+":fulfillment-item"), d.org, fid, itemID, reservationID, inventoryID, w.id, quantity); err != nil {
		return err
	}
	if err := exec(ctx, tx, `UPDATE inventory_levels SET on_hand_quantity=GREATEST(on_hand_quantity-$3,0),updated_at=NOW() WHERE organization_id=$1 AND id=$2`, d.org, inventoryID, quantity); err != nil {
		return err
	}
	return exec(ctx, tx, `INSERT INTO inventory_movements(id,organization_id,inventory_level_id,product_id,warehouse_id,movement_type,quantity_delta,fulfillment_id,reservation_id,created_by_user_id) VALUES($1,$2,$3,$4,$5,'fulfillment',$6,$7,$8,$9) ON CONFLICT(id) DO NOTHING`, stable(key+":movement"), d.org, inventoryID, productID, w.id, -quantity, fid, reservationID, user)
}

func seedTransfers(ctx context.Context, tx pgx.Tx, d seedData) error {
	if err := transferRow(ctx, tx, d, "transfer:pending", "MEL", "BNE", "pending", "TRVL-MUG-01", 4); err != nil {
		return err
	}
	if err := transferRow(ctx, tx, d, "transfer:in-transit", "MEL", "SYD", "in_transit", "HOME-LAMP-01", 3); err != nil {
		return err
	}
	return transferRow(ctx, tx, d, "transfer:completed", "SYD", "BNE", "completed", "TRVL-WEEK-01", 2)
}

func transferRow(ctx context.Context, tx pgx.Tx, d seedData, key, source, destination, status, sku string, quantity int64) error {
	id := stable(key)
	src := warehouseByCode(d.warehouses, source)
	dst := warehouseByCode(d.warehouses, destination)
	p, _ := findProduct(d.products, sku)
	srcInv := d.inventory[sku+":"+source]
	dstInv := d.inventory[sku+":"+destination]
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	hash := hashValue("transfer-hash:" + key)
	var dispatched, completed, cancelled any
	var dispatchUser, receiveUser, cancelUser any
	if status == "in_transit" || status == "completed" {
		dispatched = now.Add(time.Hour)
		dispatchUser = d.manager
	}
	if status == "completed" {
		completed = now.Add(2 * time.Hour)
		receiveUser = d.manager
	}
	if status == "cancelled" {
		cancelled = now.Add(time.Hour)
		cancelUser = d.manager
	}
	if err := exec(ctx, tx, `INSERT INTO inventory_transfers(id,organization_id,source_warehouse_id,destination_warehouse_id,status,created_by_user_id,dispatched_by_user_id,received_by_user_id,cancelled_by_user_id,created_at,dispatched_at,completed_at,cancelled_at,creation_idempotency_key,creation_request_hash,dispatch_idempotency_key,receive_idempotency_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,dispatched_at=EXCLUDED.dispatched_at,completed_at=EXCLUDED.completed_at`, id, d.org, src.id, dst.id, status, d.manager, dispatchUser, receiveUser, cancelUser, now, dispatched, completed, cancelled, key, hash, key+":dispatch", key+":receive"); err != nil {
		return err
	}
	itemID := stable(key + ":item")
	if err := exec(ctx, tx, `INSERT INTO inventory_transfer_items(id,organization_id,transfer_id,product_id,source_inventory_level_id,destination_inventory_level_id,quantity) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET quantity=EXCLUDED.quantity`, itemID, d.org, id, p.id, srcInv, dstInv, quantity); err != nil {
		return err
	}
	if status == "in_transit" || status == "completed" {
		if err := exec(ctx, tx, `UPDATE inventory_levels SET on_hand_quantity=on_hand_quantity-$3,updated_at=NOW() WHERE id=$2 AND organization_id=$1`, d.org, srcInv, quantity); err != nil {
			return err
		}
		if err := exec(ctx, tx, `INSERT INTO inventory_movements(id,organization_id,inventory_level_id,product_id,warehouse_id,movement_type,quantity_delta,transfer_id,transfer_item_id,created_by_user_id) VALUES($1,$2,$3,$4,$5,'transfer_out',$6,$7,$8,$9) ON CONFLICT(id) DO NOTHING`, stable(key+":out"), d.org, srcInv, p.id, src.id, -quantity, id, itemID, d.manager); err != nil {
			return err
		}
	}
	if status == "completed" {
		if err := exec(ctx, tx, `UPDATE inventory_levels SET on_hand_quantity=on_hand_quantity+$3,updated_at=NOW() WHERE id=$2 AND organization_id=$1`, d.org, dstInv, quantity); err != nil {
			return err
		}
		if err := exec(ctx, tx, `INSERT INTO inventory_movements(id,organization_id,inventory_level_id,product_id,warehouse_id,movement_type,quantity_delta,transfer_id,transfer_item_id,created_by_user_id) VALUES($1,$2,$3,$4,$5,'transfer_in',$6,$7,$8,$9) ON CONFLICT(id) DO NOTHING`, stable(key+":in"), d.org, dstInv, p.id, dst.id, quantity, id, itemID, d.manager); err != nil {
			return err
		}
	}
	return nil
}

func seedAudit(ctx context.Context, tx pgx.Tx, d seedData) error {
	for _, item := range []struct {
		key, event, resource string
		actor                uuid.UUID
		meta                 map[string]any
	}{
		{"order:pending", "order.created", "order", d.owner, map[string]any{"status": "pending"}},
		{"order:paid", "order.paid", "order", d.owner, map[string]any{"status": "paid"}},
		{"order:fulfilled", "order.fulfilled", "order", d.owner, map[string]any{"status": "fulfilled"}},
		{"transfer:pending", "transfer.created", "transfer", d.manager, map[string]any{"status": "pending"}},
		{"transfer:in-transit", "transfer.dispatched", "transfer", d.manager, map[string]any{"status": "in_transit"}},
		{"transfer:completed", "transfer.received", "transfer", d.manager, map[string]any{"status": "completed"}},
	} {
		if err := auditInsert(ctx, tx, d, item.key, item.event, item.resource, item.actor, item.meta); err != nil {
			return err
		}
	}
	return nil
}

func auditInsert(ctx context.Context, tx pgx.Tx, d seedData, key, event, resource string, actor uuid.UUID, metadata map[string]any) error {
	if event == "order.paid" {
		var amount int64
		if err := tx.QueryRow(ctx, `SELECT subtotal_minor FROM orders WHERE organization_id=$1 AND id=$2`, d.org, stable(key)).Scan(&amount); err != nil {
			return err
		}
		metadata["amount_minor"] = amount
	}
	data, _ := json.Marshal(metadata)
	return exec(ctx, tx, `INSERT INTO audit_events(organization_id,event_type,resource_type,resource_id,actor_type,actor_user_id,source_type,source_id,metadata) VALUES($1,$2,$3,$4,'user',$5,$3,$4,$6) ON CONFLICT(organization_id,source_type,source_id,event_type) DO NOTHING`, d.org, event, resource, stable(key), actor, data)
}
func exec(ctx context.Context, tx pgx.Tx, query string, args ...any) error {
	if _, err := tx.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("seed SQL %q: %w", query, err)
	}
	return nil
}

func hashValue(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func findProduct(products []product, sku string) (product, bool) {
	for _, p := range products {
		if p.sku == sku {
			return p, true
		}
	}
	return product{}, false
}
func warehouseByCode(items []warehouse, code string) warehouse {
	for _, w := range items {
		if w.code == code {
			return w
		}
	}
	panic("unknown warehouse " + code)
}

func safeSeedEnvironment(value string) bool {
	return value == "development" || value == "test" || value == "verification"
}
