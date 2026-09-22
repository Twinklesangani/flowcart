package main

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Runs entirely in a rolled-back transaction. Use only a disposable test database.
func TestSeedAmountsExpiryStableIdentitiesAndIgnoresUnrelatedRows(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO organizations(id,name,slug) VALUES($1,'Unrelated seed test organization',$2)`, uuid.New(), "unrelated-seed-test-"+uuid.New().String()); err != nil {
		t.Fatal(err)
	}
	d := makeSeedData()
	tables := []string{"users", "organizations", "organization_members", "products", "warehouses", "inventory_levels", "orders", "order_items", "inventory_reservations", "payments", "fulfillments", "fulfillment_items", "inventory_movements", "inventory_transfers", "inventory_transfer_items", "audit_events"}
	seedUserIDs := []uuid.UUID{d.owner, d.manager, d.viewer}
	var first []string
	for run := 0; run < 2; run++ {
		for _, step := range []func() error{
			func() error { return seedBase(ctx, tx, d, "test-hash") },
			func() error { return seedInventory(ctx, tx, d) },
			func() error { return seedOrders(ctx, tx, d) },
			func() error { return seedTransfers(ctx, tx, d) },
			func() error { return seedAudit(ctx, tx, d) },
		} {
			if err := step(); err != nil {
				t.Fatal(err)
			}
		}
		var snapshot []string
		for _, table := range tables {
			var ids string
			switch table {
			case "users":
				if err := tx.QueryRow(ctx, `SELECT COALESCE(string_agg(id::text,',' ORDER BY id),'') FROM users WHERE id = ANY($1)`, seedUserIDs).Scan(&ids); err != nil {
					t.Fatal(err)
				}
			case "organizations":
				if err := tx.QueryRow(ctx, `SELECT COALESCE(string_agg(id::text,',' ORDER BY id),'') FROM organizations WHERE id=$1`, d.org).Scan(&ids); err != nil {
					t.Fatal(err)
				}
			case "organization_members":
				if err := tx.QueryRow(ctx, `SELECT COALESCE(string_agg(id::text,',' ORDER BY id),'') FROM organization_members WHERE organization_id=$1 AND user_id = ANY($2)`, d.org, seedUserIDs).Scan(&ids); err != nil {
					t.Fatal(err)
				}
			default:
				if err := tx.QueryRow(ctx, `SELECT COALESCE(string_agg(id::text,',' ORDER BY id),'') FROM `+table+` WHERE organization_id=$1`, d.org).Scan(&ids); err != nil {
					t.Fatal(err)
				}
			}
			snapshot = append(snapshot, ids)
		}
		if run == 0 {
			first = snapshot
		} else if !reflect.DeepEqual(first, snapshot) {
			t.Fatal("second seed changed logical identities")
		}
		for _, query := range []string{
			`SELECT count(*) FROM orders o WHERE organization_id=$1 AND subtotal_minor <> (SELECT sum(line_total_minor) FROM order_items i WHERE i.order_id=o.id)`,
			`SELECT count(*) FROM payments p JOIN orders o ON o.id=p.order_id WHERE p.organization_id=$1 AND p.amount_minor<>o.subtotal_minor`,
			`SELECT count(*) FROM inventory_reservations WHERE organization_id=$1 AND status='active' AND expires_at<=now()`,
			`SELECT count(*) FROM audit_events a JOIN orders o ON o.id=a.resource_id WHERE a.organization_id=$1 AND a.event_type='order.paid' AND (a.metadata->>'amount_minor')::bigint<>o.subtotal_minor`,
		} {
			var invalid int
			if err := tx.QueryRow(ctx, query, d.org).Scan(&invalid); err != nil {
				t.Fatal(err)
			}
			if invalid != 0 {
				t.Fatalf("%d inconsistent seed rows: %s", invalid, query)
			}
		}
		var items, reservations, quantity int
		if err := tx.QueryRow(ctx, `SELECT count(DISTINCT i.id),count(r.id),sum(r.quantity) FROM order_items i JOIN inventory_reservations r ON r.order_item_id=i.id WHERE i.order_id=$1`, stable("order:auto-multi")).Scan(&items, &reservations, &quantity); err != nil {
			t.Fatal(err)
		}
		if items != 1 || reservations != 2 || quantity != 4 {
			t.Fatalf("split shape = %d/%d/%d", items, reservations, quantity)
		}
	}
}
