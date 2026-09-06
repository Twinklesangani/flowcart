package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type repository interface {
	Create(context.Context, uuid.UUID, CreateInput) (InventoryLevel, error)
	List(context.Context, uuid.UUID) ([]InventoryLevel, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (InventoryLevel, error)
	Adjust(context.Context, uuid.UUID, uuid.UUID, int64) (InventoryLevel, error)
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const inventoryColumns = `i.id, i.organization_id, i.product_id, p.sku, p.name,
	i.warehouse_id, w.code, w.name, i.on_hand_quantity, i.created_at, i.updated_at`

func scanInventory(row pgx.Row) (InventoryLevel, error) {
	var item InventoryLevel
	err := row.Scan(&item.ID, &item.OrganizationID, &item.ProductID, &item.ProductSKU, &item.ProductName,
		&item.WarehouseID, &item.WarehouseCode, &item.WarehouseName, &item.OnHandQuantity, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *Repository) Create(ctx context.Context, organizationID uuid.UUID, input CreateInput) (InventoryLevel, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("begin inventory creation: %w", err)
	}
	defer tx.Rollback(ctx)

	var active bool
	err = tx.QueryRow(ctx, `SELECT is_active FROM products WHERE organization_id=$1 AND id=$2`, organizationID, input.ProductID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return InventoryLevel{}, ErrProductNotFound
	}
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("check product: %w", err)
	}
	if !active {
		return InventoryLevel{}, ErrInactiveProduct
	}
	err = tx.QueryRow(ctx, `SELECT is_active FROM warehouses WHERE organization_id=$1 AND id=$2`, organizationID, input.WarehouseID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return InventoryLevel{}, ErrWarehouseNotFound
	}
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("check warehouse: %w", err)
	}
	if !active {
		return InventoryLevel{}, ErrInactiveWarehouse
	}

	var itemID uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO inventory_levels (organization_id, product_id, warehouse_id, on_hand_quantity)
		VALUES ($1,$2,$3,$4)
		RETURNING id`, organizationID, input.ProductID, input.WarehouseID, input.OnHandQuantity).Scan(&itemID)
	if isUniqueError(err) {
		return InventoryLevel{}, ErrInventoryExists
	}
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("create inventory: %w", err)
	}
	item, err := r.getTx(ctx, tx, organizationID, itemID)
	if err != nil {
		return InventoryLevel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return InventoryLevel{}, fmt.Errorf("commit inventory creation: %w", err)
	}
	return item, nil
}

func (r *Repository) List(ctx context.Context, organizationID uuid.UUID) ([]InventoryLevel, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+inventoryColumns+` FROM inventory_levels i
		JOIN products p ON p.id=i.product_id AND p.organization_id=i.organization_id
		JOIN warehouses w ON w.id=i.warehouse_id AND w.organization_id=i.organization_id
		WHERE i.organization_id=$1 ORDER BY p.name, w.name`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list inventory: %w", err)
	}
	defer rows.Close()
	items := make([]InventoryLevel, 0)
	for rows.Next() {
		item, scanErr := scanInventory(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan inventory: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list inventory: %w", err)
	}
	return items, nil
}

func (r *Repository) Get(ctx context.Context, organizationID, id uuid.UUID) (InventoryLevel, error) {
	item, err := scanInventory(r.pool.QueryRow(ctx, `SELECT `+inventoryColumns+` FROM inventory_levels i
		JOIN products p ON p.id=i.product_id AND p.organization_id=i.organization_id
		JOIN warehouses w ON w.id=i.warehouse_id AND w.organization_id=i.organization_id
		WHERE i.organization_id=$1 AND i.id=$2`, organizationID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return InventoryLevel{}, ErrNotFound
	}
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("get inventory: %w", err)
	}
	return item, nil
}

func (r *Repository) Adjust(ctx context.Context, organizationID, id uuid.UUID, delta int64) (InventoryLevel, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("begin stock adjustment: %w", err)
	}
	defer tx.Rollback(ctx)
	var current int64
	err = tx.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return InventoryLevel{}, ErrNotFound
	}
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("lock inventory: %w", err)
	}
	if delta < 0 && (delta == -1<<63 || current < -delta) {
		return InventoryLevel{}, ErrInsufficientStock
	}
	if _, err := tx.Exec(ctx, `UPDATE inventory_levels SET on_hand_quantity=on_hand_quantity+$3, updated_at=NOW() WHERE organization_id=$1 AND id=$2`, organizationID, id, delta); err != nil {
		return InventoryLevel{}, fmt.Errorf("adjust inventory: %w", err)
	}
	item, err := r.getTx(ctx, tx, organizationID, id)
	if err != nil {
		return InventoryLevel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return InventoryLevel{}, fmt.Errorf("commit stock adjustment: %w", err)
	}
	return item, nil
}

func (r *Repository) getTx(ctx context.Context, tx pgx.Tx, organizationID, id uuid.UUID) (InventoryLevel, error) {
	item, err := scanInventory(tx.QueryRow(ctx, `SELECT `+inventoryColumns+` FROM inventory_levels i
		JOIN products p ON p.id=i.product_id AND p.organization_id=i.organization_id
		JOIN warehouses w ON w.id=i.warehouse_id AND w.organization_id=i.organization_id
		WHERE i.organization_id=$1 AND i.id=$2`, organizationID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return InventoryLevel{}, ErrNotFound
	}
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("read inventory transaction: %w", err)
	}
	return item, nil
}

func isUniqueError(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}
