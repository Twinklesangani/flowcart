package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	Reserve(context.Context, uuid.UUID, uuid.UUID, int64) (Reservation, error)
	Release(context.Context, uuid.UUID, uuid.UUID) (Reservation, error)
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const inventoryColumns = `i.id, i.organization_id, i.product_id, p.sku, p.name,
	i.warehouse_id, w.code, w.name, i.on_hand_quantity,
	COALESCE((SELECT SUM(r.quantity) FROM inventory_reservations r
		WHERE r.organization_id=i.organization_id AND r.inventory_level_id=i.id
		AND r.status='active' AND r.expires_at > NOW()), 0),
	GREATEST(i.on_hand_quantity - COALESCE((SELECT SUM(r.quantity) FROM inventory_reservations r
		WHERE r.organization_id=i.organization_id AND r.inventory_level_id=i.id
		AND r.status='active' AND r.expires_at > NOW()), 0), 0),
	i.created_at, i.updated_at`

func scanInventory(row pgx.Row) (InventoryLevel, error) {
	var item InventoryLevel
	err := row.Scan(&item.ID, &item.OrganizationID, &item.ProductID, &item.ProductSKU, &item.ProductName,
		&item.WarehouseID, &item.WarehouseCode, &item.WarehouseName, &item.OnHandQuantity,
		&item.ReservedQuantity, &item.AvailableQuantity, &item.CreatedAt, &item.UpdatedAt)
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
	if err := expireReservationsTx(ctx, tx, organizationID, id); err != nil {
		return InventoryLevel{}, err
	}
	reserved, err := reservedQuantityTx(ctx, tx, organizationID, id)
	if err != nil {
		return InventoryLevel{}, err
	}
	if delta < 0 && (delta == -1<<63 || current < -delta) {
		return InventoryLevel{}, ErrInsufficientStock
	}
	if current+delta < reserved {
		return InventoryLevel{}, ErrStockBelowReserved
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

func (r *Repository) Reserve(ctx context.Context, organizationID, inventoryID uuid.UUID, quantity int64) (Reservation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Reservation{}, fmt.Errorf("begin reservation: %w", err)
	}
	defer tx.Rollback(ctx)

	var onHand int64
	var productActive, warehouseActive bool
	err = tx.QueryRow(ctx, `SELECT i.on_hand_quantity, p.is_active, w.is_active
		FROM inventory_levels i
		JOIN products p ON p.id=i.product_id AND p.organization_id=i.organization_id
		JOIN warehouses w ON w.id=i.warehouse_id AND w.organization_id=i.organization_id
		WHERE i.organization_id=$1 AND i.id=$2
		FOR UPDATE OF i`, organizationID, inventoryID).Scan(&onHand, &productActive, &warehouseActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrNotFound
	}
	if err != nil {
		return Reservation{}, fmt.Errorf("lock inventory for reservation: %w", err)
	}
	if !productActive {
		return Reservation{}, ErrInactiveProduct
	}
	if !warehouseActive {
		return Reservation{}, ErrInactiveWarehouse
	}
	if err := expireReservationsTx(ctx, tx, organizationID, inventoryID); err != nil {
		return Reservation{}, err
	}
	reserved, err := reservedQuantityTx(ctx, tx, organizationID, inventoryID)
	if err != nil {
		return Reservation{}, err
	}
	if quantity > onHand-reserved {
		return Reservation{}, ErrInsufficientAvailableStock
	}

	reservation, err := scanReservation(tx.QueryRow(ctx, `INSERT INTO inventory_reservations
		(organization_id, inventory_level_id, quantity, expires_at)
		VALUES ($1,$2,$3,NOW() + ($4 * INTERVAL '1 second'))
		RETURNING id, organization_id, inventory_level_id, quantity, status, expires_at, created_at, released_at, expired_at`,
		organizationID, inventoryID, quantity, int64(ReservationTTL/time.Second)))
	if err != nil {
		return Reservation{}, fmt.Errorf("create reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Reservation{}, fmt.Errorf("commit reservation: %w", err)
	}
	return reservation, nil
}

func (r *Repository) Release(ctx context.Context, organizationID, reservationID uuid.UUID) (Reservation, error) {
	var inventoryID uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT inventory_level_id FROM inventory_reservations WHERE organization_id=$1 AND id=$2`, organizationID, reservationID).Scan(&inventoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrReservationNotFound
	}
	if err != nil {
		return Reservation{}, fmt.Errorf("find reservation inventory: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Reservation{}, fmt.Errorf("begin reservation release: %w", err)
	}
	defer tx.Rollback(ctx)
	var lockedInventoryID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM inventory_levels WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, inventoryID).Scan(&lockedInventoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrReservationNotFound
	}
	if err != nil {
		return Reservation{}, fmt.Errorf("lock reservation inventory: %w", err)
	}
	if err := expireReservationsTx(ctx, tx, organizationID, inventoryID); err != nil {
		return Reservation{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE inventory_reservations
		SET status=CASE WHEN expires_at <= NOW() THEN 'expired' ELSE 'released' END,
			released_at=CASE WHEN expires_at > NOW() THEN NOW() ELSE released_at END,
			expired_at=CASE WHEN expires_at <= NOW() THEN NOW() ELSE expired_at END
		WHERE organization_id=$1 AND id=$2 AND inventory_level_id=$3 AND status='active'`, organizationID, reservationID, lockedInventoryID)
	if err != nil {
		return Reservation{}, fmt.Errorf("release reservation: %w", err)
	}
	reservation, err := getReservationTx(ctx, tx, organizationID, reservationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrReservationNotFound
	}
	if err != nil {
		return Reservation{}, fmt.Errorf("read released reservation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Reservation{}, fmt.Errorf("commit reservation release: %w", err)
	}
	return reservation, nil
}

const reservationColumns = `id, organization_id, inventory_level_id, quantity, status, expires_at, created_at, released_at, expired_at`

func scanReservation(row pgx.Row) (Reservation, error) {
	var reservation Reservation
	err := row.Scan(&reservation.ID, &reservation.OrganizationID, &reservation.InventoryLevelID,
		&reservation.Quantity, &reservation.Status, &reservation.ExpiresAt, &reservation.CreatedAt,
		&reservation.ReleasedAt, &reservation.ExpiredAt)
	return reservation, err
}

func getReservationTx(ctx context.Context, tx pgx.Tx, organizationID, reservationID uuid.UUID) (Reservation, error) {
	return scanReservation(tx.QueryRow(ctx, `SELECT `+reservationColumns+` FROM inventory_reservations WHERE organization_id=$1 AND id=$2`, organizationID, reservationID))
}

func expireReservationsTx(ctx context.Context, tx pgx.Tx, organizationID, inventoryID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE inventory_reservations SET status='expired', expired_at=NOW()
		WHERE organization_id=$1 AND inventory_level_id=$2 AND status='active' AND expires_at <= NOW()`, organizationID, inventoryID); err != nil {
		return fmt.Errorf("expire reservations: %w", err)
	}
	return nil
}

func reservedQuantityTx(ctx context.Context, tx pgx.Tx, organizationID, inventoryID uuid.UUID) (int64, error) {
	var reserved int64
	err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(quantity),0) FROM inventory_reservations
		WHERE organization_id=$1 AND inventory_level_id=$2 AND status='active' AND expires_at > NOW()`, organizationID, inventoryID).Scan(&reserved)
	if err != nil {
		return 0, fmt.Errorf("calculate reserved quantity: %w", err)
	}
	return reserved, nil
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
