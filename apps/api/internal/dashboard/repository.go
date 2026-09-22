package dashboard

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Metrics struct {
	OrderCount      int               `json:"order_count"`
	OpenOrderCount  int               `json:"open_order_count"`
	PaidOrderCount  int               `json:"paid_order_count"`
	InventoryCount  int               `json:"inventory_count"`
	WarehouseCount  int               `json:"warehouse_count"`
	LowStockCount   int               `json:"low_stock_count"`
	OutOfStockCount int               `json:"out_of_stock_count"`
	TransferCount   int               `json:"transfer_count"`
	Warehouses      []WarehouseMetric `json:"warehouses"`
}

type WarehouseMetric struct {
	ID                uuid.UUID `json:"id"`
	Code              string    `json:"code"`
	Name              string    `json:"name"`
	InventoryCount    int       `json:"inventory_count"`
	AvailableQuantity int64     `json:"available_quantity"`
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Metrics(ctx context.Context, organizationID uuid.UUID) (Metrics, error) {
	var metrics Metrics
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)::int, COUNT(*) FILTER (WHERE status='pending')::int, COUNT(*) FILTER (WHERE status='paid')::int FROM orders WHERE organization_id=$1`, organizationID).Scan(&metrics.OrderCount, &metrics.OpenOrderCount, &metrics.PaidOrderCount); err != nil {
		return Metrics{}, fmt.Errorf("dashboard order metrics: %w", err)
	}
	if err := r.pool.QueryRow(ctx, `WITH availability AS (
		SELECT i.id,i.warehouse_id,i.on_hand_quantity,i.reorder_point,i.target_stock_level,
			i.on_hand_quantity-COALESCE((SELECT SUM(r.quantity) FROM inventory_reservations r WHERE r.organization_id=i.organization_id AND r.inventory_level_id=i.id AND ((r.status='active' AND r.expires_at>statement_timestamp()) OR r.status IN ('payment_held','committed'))),0) AS available
		FROM inventory_levels i JOIN products p ON p.organization_id=i.organization_id AND p.id=i.product_id AND p.is_active JOIN warehouses w ON w.organization_id=i.organization_id AND w.id=i.warehouse_id AND w.is_active
		WHERE i.organization_id=$1
	)
	SELECT COUNT(*)::int, COUNT(DISTINCT warehouse_id)::int,
		COUNT(*) FILTER (WHERE reorder_point IS NOT NULL AND target_stock_level IS NOT NULL AND available<=reorder_point AND available>0)::int,
		COUNT(*) FILTER (WHERE reorder_point IS NOT NULL AND target_stock_level IS NOT NULL AND available<=0)::int
	FROM availability`, organizationID).Scan(&metrics.InventoryCount, &metrics.WarehouseCount, &metrics.LowStockCount, &metrics.OutOfStockCount); err != nil {
		return Metrics{}, fmt.Errorf("dashboard inventory metrics: %w", err)
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM inventory_transfers WHERE organization_id=$1`, organizationID).Scan(&metrics.TransferCount); err != nil {
		return Metrics{}, fmt.Errorf("dashboard transfer metrics: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT w.id,w.code,w.name,COALESCE(a.inventory_count,0),COALESCE(a.available_quantity,0)
		FROM warehouses w
		LEFT JOIN (
			SELECT i.warehouse_id,COUNT(*)::int AS inventory_count,
				COALESCE(SUM(i.on_hand_quantity-COALESCE((SELECT SUM(r.quantity) FROM inventory_reservations r WHERE r.organization_id=i.organization_id AND r.inventory_level_id=i.id AND ((r.status='active' AND r.expires_at>statement_timestamp()) OR r.status IN ('payment_held','committed'))),0)),0)::bigint AS available_quantity
			FROM inventory_levels i
			JOIN products p ON p.organization_id=i.organization_id AND p.id=i.product_id AND p.is_active
			WHERE i.organization_id=$1
			GROUP BY i.warehouse_id
		) a ON a.warehouse_id=w.id
		WHERE w.organization_id=$1 AND w.is_active
		ORDER BY w.name,w.id`, organizationID)
	if err != nil {
		return Metrics{}, fmt.Errorf("dashboard warehouse metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var warehouse WarehouseMetric
		if err := rows.Scan(&warehouse.ID, &warehouse.Code, &warehouse.Name, &warehouse.InventoryCount, &warehouse.AvailableQuantity); err != nil {
			return Metrics{}, fmt.Errorf("scan dashboard warehouse metrics: %w", err)
		}
		metrics.Warehouses = append(metrics.Warehouses, warehouse)
	}
	if err := rows.Err(); err != nil {
		return Metrics{}, fmt.Errorf("dashboard warehouse metrics: %w", err)
	}
	return metrics, nil
}
