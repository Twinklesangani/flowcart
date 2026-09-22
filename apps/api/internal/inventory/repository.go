package inventory

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"flowcart/apps/api/internal/audit"
	"flowcart/apps/api/internal/pagination"

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
		AND ((r.status='active' AND r.expires_at > statement_timestamp()) OR r.status IN ('payment_held','committed'))), 0),
	GREATEST(i.on_hand_quantity - COALESCE((SELECT SUM(r.quantity) FROM inventory_reservations r
		WHERE r.organization_id=i.organization_id AND r.inventory_level_id=i.id
		AND ((r.status='active' AND r.expires_at > statement_timestamp()) OR r.status IN ('payment_held','committed'))), 0), 0),
	i.reorder_point, i.target_stock_level, i.created_at, i.updated_at`

func scanInventory(row pgx.Row) (InventoryLevel, error) {
	var item InventoryLevel
	err := row.Scan(&item.ID, &item.OrganizationID, &item.ProductID, &item.ProductSKU, &item.ProductName,
		&item.WarehouseID, &item.WarehouseCode, &item.WarehouseName, &item.OnHandQuantity,
		&item.ReservedQuantity, &item.AvailableQuantity, &item.ReorderPoint, &item.TargetStockLevel,
		&item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *Repository) Create(ctx context.Context, organizationID uuid.UUID, input CreateInput) (InventoryLevel, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("begin inventory creation: %w", err)
	}
	defer tx.Rollback(ctx)

	var active bool
	err = tx.QueryRow(ctx, `SELECT is_active FROM products WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, input.ProductID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return InventoryLevel{}, ErrProductNotFound
	}
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("check product: %w", err)
	}
	if !active {
		return InventoryLevel{}, ErrInactiveProduct
	}
	err = tx.QueryRow(ctx, `SELECT is_active FROM warehouses WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, input.WarehouseID).Scan(&active)
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

func (r *Repository) ListPage(ctx context.Context, organizationID uuid.UUID, limit int, cursor pagination.Cursor, warehouseID, productID uuid.UUID) (ListPage, error) {
	where := "WHERE i.organization_id=$1"
	args := []any{organizationID}
	arg := 2
	if warehouseID != uuid.Nil {
		where += fmt.Sprintf(" AND i.warehouse_id=$%d", arg)
		args = append(args, warehouseID)
		arg++
	}
	if productID != uuid.Nil {
		where += fmt.Sprintf(" AND i.product_id=$%d", arg)
		args = append(args, productID)
		arg++
	}
	if cursor.ID != uuid.Nil {
		where += fmt.Sprintf(" AND (i.created_at,i.id)<($%d,$%d)", arg, arg+1)
		args = append(args, cursor.CreatedAt, cursor.ID)
		arg += 2
	}
	args = append(args, limit+1)
	rows, err := r.pool.Query(ctx, `SELECT `+inventoryColumns+` FROM inventory_levels i JOIN products p ON p.id=i.product_id AND p.organization_id=i.organization_id JOIN warehouses w ON w.id=i.warehouse_id AND w.organization_id=i.organization_id `+where+fmt.Sprintf(` ORDER BY i.created_at DESC,i.id DESC LIMIT $%d`, arg), args...)
	if err != nil {
		return ListPage{}, fmt.Errorf("list inventory: %w", err)
	}
	defer rows.Close()
	items := make([]InventoryLevel, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanInventory(rows)
		if scanErr != nil {
			return ListPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListPage{}, err
	}
	page := ListPage{Inventory: items}
	if len(items) > limit {
		page.Inventory = items[:limit]
		page.HasMore = true
		last := page.Inventory[len(page.Inventory)-1]
		page.NextCursor = pagination.EncodeCursor(pagination.Cursor{OrganizationID: organizationID, CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return page, nil
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

func (r *Repository) UpdateReplenishmentPolicy(ctx context.Context, organizationID, id uuid.UUID, input ReplenishmentPolicyInput) (InventoryLevel, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("begin replenishment policy update: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `UPDATE inventory_levels SET reorder_point=$3,target_stock_level=$4,updated_at=NOW()
		WHERE organization_id=$1 AND id=$2`, organizationID, id, input.ReorderPoint, input.TargetStockLevel)
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("update replenishment policy: %w", err)
	}
	if result.RowsAffected() == 0 {
		return InventoryLevel{}, ErrNotFound
	}
	writer := audit.Writer{}
	if err := writer.AppendEvent(ctx, tx, audit.Event{
		OrganizationID: organizationID,
		EventType:      "replenishment.policy_updated",
		ResourceType:   "inventory_level",
		ResourceID:     id,
		ActorType:      "system",
		SourceType:     "inventory_level",
		SourceID:       id,
		Metadata:       map[string]any{"reorder_point": input.ReorderPoint, "target_stock_level": input.TargetStockLevel},
	}); err != nil {
		return InventoryLevel{}, fmt.Errorf("append replenishment policy audit event: %w", err)
	}
	item, err := r.getTx(ctx, tx, organizationID, id)
	if err != nil {
		return InventoryLevel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return InventoryLevel{}, fmt.Errorf("commit replenishment policy update: %w", err)
	}
	return item, nil
}

type donorCandidate struct {
	TargetID           uuid.UUID
	ProductID          uuid.UUID
	WarehouseID        uuid.UUID
	WarehouseCode      string
	InventoryLevelID   uuid.UUID
	EffectiveAvailable int64
	ReorderPoint       int64
}

const replenishmentDonorLimit = 6
const replenishmentDonorCandidateBudget = 32

func (r *Repository) LowStock(ctx context.Context, organizationID uuid.UUID, limit int) ([]LowStockItem, []donorCandidate, error) {
	items, donors, _, _, err := r.LowStockPage(ctx, organizationID, limit, lowStockCursor{})
	return items, donors, err
}

func (r *Repository) LowStockPage(ctx context.Context, organizationID uuid.UUID, limit int, cursor lowStockCursor) ([]LowStockItem, []donorCandidate, *lowStockCursor, bool, error) {
	where := "WHERE effective_available <= reorder_point"
	args := []any{organizationID}
	arg := 2
	if cursor.ID != uuid.Nil {
		where += " AND (priority,effective_available,id)>($2,$3,$4)"
		args = append(args, cursor.Priority, cursor.Available, cursor.ID)
		arg = 5
	}
	args = append(args, limit+1)
	rows, err := r.pool.Query(ctx, `WITH availability AS (
		SELECT i.id,i.organization_id,i.product_id,p.sku,p.name AS product_name,i.warehouse_id,w.code,w.name AS warehouse_name,
			i.on_hand_quantity,i.reorder_point,i.target_stock_level,i.created_at,i.updated_at,
			COALESCE(SUM(r.quantity) FILTER (WHERE ((r.status='active' AND r.expires_at > statement_timestamp()) OR r.status IN ('payment_held','committed'))),0)::BIGINT AS reserved_quantity
		FROM inventory_levels i
		JOIN products p ON p.organization_id=i.organization_id AND p.id=i.product_id AND p.is_active
		JOIN warehouses w ON w.organization_id=i.organization_id AND w.id=i.warehouse_id AND w.is_active
		LEFT JOIN inventory_reservations r ON r.organization_id=i.organization_id AND r.inventory_level_id=i.id
		WHERE i.organization_id=$1 AND i.reorder_point IS NOT NULL AND i.target_stock_level IS NOT NULL
		GROUP BY i.id,i.organization_id,i.product_id,p.sku,p.name,i.warehouse_id,w.code,w.name,i.on_hand_quantity,i.reorder_point,i.target_stock_level,i.created_at,i.updated_at
	), low AS (
		SELECT *, on_hand_quantity-reserved_quantity AS effective_available,
			CASE WHEN on_hand_quantity-reserved_quantity <= 0 THEN 0 ELSE 1 END AS priority
		FROM availability
		WHERE on_hand_quantity-reserved_quantity <= reorder_point
	)
	SELECT id,organization_id,product_id,sku,product_name,warehouse_id,code,warehouse_name,on_hand_quantity,
		reserved_quantity,effective_available,reorder_point,target_stock_level,created_at,updated_at,priority
		FROM low
		`+where+`
		ORDER BY priority,effective_available,id
		LIMIT $`+fmt.Sprint(arg), args...)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("list low stock: %w", err)
	}
	defer rows.Close()
	items := make([]LowStockItem, 0, limit+1)
	priorities := make([]int, 0, limit+1)
	productIDs := make([]uuid.UUID, 0)
	targetIDs := make([]uuid.UUID, 0, len(items))
	seenProducts := make(map[uuid.UUID]bool)
	for rows.Next() {
		var item LowStockItem
		var priority int
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.ProductID, &item.ProductSKU, &item.ProductName, &item.WarehouseID, &item.WarehouseCode, &item.WarehouseName, &item.OnHandQuantity, &item.ReservedQuantity, &item.AvailableQuantity, &item.ReorderPoint, &item.TargetStockLevel, &item.CreatedAt, &item.UpdatedAt, &priority); err != nil {
			return nil, nil, nil, false, fmt.Errorf("scan low stock: %w", err)
		}
		priorities = append(priorities, priority)
		if item.AvailableQuantity <= 0 {
			item.Status = StatusOutOfStock
		} else {
			item.Status = StatusLow
		}
		items = append(items, item)
		targetIDs = append(targetIDs, item.ID)
		if !seenProducts[item.ProductID] {
			seenProducts[item.ProductID] = true
			productIDs = append(productIDs, item.ProductID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, false, fmt.Errorf("list low stock: %w", err)
	}
	var next *lowStockCursor
	if len(items) > limit {
		lastIndex := limit - 1
		next = &lowStockCursor{Priority: priorities[lastIndex], Available: items[lastIndex].AvailableQuantity, ID: items[lastIndex].ID}
		items = items[:limit]
		priorities = priorities[:limit]
	}
	if len(productIDs) == 0 {
		return items, nil, next, false, nil
	}
	donorRows, err := r.pool.Query(ctx, `WITH candidate_ids AS (
		SELECT target.id AS target_id, candidate.id AS donor_id
		FROM inventory_levels target
		CROSS JOIN LATERAL (
			SELECT donor.id
			FROM inventory_levels donor
			JOIN products p ON p.organization_id=donor.organization_id AND p.id=donor.product_id AND p.is_active
			JOIN warehouses w ON w.organization_id=donor.organization_id AND w.id=donor.warehouse_id AND w.is_active
			WHERE donor.organization_id=$1 AND donor.product_id=target.product_id AND donor.warehouse_id<>target.warehouse_id
				AND donor.reorder_point IS NOT NULL AND donor.target_stock_level IS NOT NULL
			ORDER BY donor.warehouse_id
			LIMIT $3
		) candidate
		WHERE target.organization_id=$1 AND target.id=ANY($2)
	), bounded AS (
		SELECT DISTINCT c.target_id, i.product_id, i.warehouse_id, w.code, i.id, i.on_hand_quantity, i.reorder_point
		FROM candidate_ids c
		JOIN inventory_levels i ON i.organization_id=$1 AND i.id=c.donor_id
		JOIN warehouses w ON w.organization_id=i.organization_id AND w.id=i.warehouse_id AND w.is_active
	), ranked AS (
		SELECT b.target_id,b.product_id,b.warehouse_id,b.code,b.id,
			b.on_hand_quantity-COALESCE(SUM(r.quantity) FILTER (WHERE ((r.status='active' AND r.expires_at > statement_timestamp()) OR r.status IN ('payment_held','committed'))),0) AS effective_available,
			b.reorder_point
		FROM bounded b
		LEFT JOIN inventory_reservations r ON r.organization_id=$1 AND r.inventory_level_id=b.id
		GROUP BY b.target_id,b.product_id,b.warehouse_id,b.code,b.id,b.on_hand_quantity,b.reorder_point
	)
	SELECT target_id,product_id,warehouse_id,code,id,effective_available,reorder_point
	FROM ranked
	ORDER BY target_id, effective_available DESC, code ASC, id ASC`, organizationID, targetIDs, replenishmentDonorCandidateBudget)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("list replenishment donors: %w", err)
	}
	defer donorRows.Close()
	donors := make([]donorCandidate, 0)
	donorGroups := make(map[uuid.UUID][]donorCandidate)
	for donorRows.Next() {
		var donor donorCandidate
		if err := donorRows.Scan(&donor.TargetID, &donor.ProductID, &donor.WarehouseID, &donor.WarehouseCode, &donor.InventoryLevelID, &donor.EffectiveAvailable, &donor.ReorderPoint); err != nil {
			return nil, nil, nil, false, fmt.Errorf("scan replenishment donor: %w", err)
		}
		donorGroups[donor.TargetID] = append(donorGroups[donor.TargetID], donor)
	}
	if err := donorRows.Err(); err != nil {
		return nil, nil, nil, false, fmt.Errorf("scan replenishment donors: %w", err)
	}
	donorsTruncated := false
	for _, targetDonors := range donorGroups {
		qualifying := make([]donorCandidate, 0, len(targetDonors))
		for _, donor := range targetDonors {
			transferable := donor.EffectiveAvailable - donor.ReorderPoint
			if transferable <= 0 {
				continue
			}
			donor.EffectiveAvailable = transferable
			qualifying = append(qualifying, donor)
		}
		sort.Slice(qualifying, func(i, j int) bool {
			if qualifying[i].EffectiveAvailable != qualifying[j].EffectiveAvailable {
				return qualifying[i].EffectiveAvailable > qualifying[j].EffectiveAvailable
			}
			if qualifying[i].WarehouseCode != qualifying[j].WarehouseCode {
				return qualifying[i].WarehouseCode < qualifying[j].WarehouseCode
			}
			return qualifying[i].WarehouseID.String() < qualifying[j].WarehouseID.String()
		})
		if len(qualifying) > replenishmentDonorLimit+1 {
			donorsTruncated = true
			qualifying = qualifying[:replenishmentDonorLimit+1]
		}
		donors = append(donors, qualifying...)
	}
	return items, donors, next, donorsTruncated, nil
}

func (r *Repository) Adjust(ctx context.Context, organizationID, id uuid.UUID, delta int64) (InventoryLevel, error) {
	return r.adjust(ctx, organizationID, uuid.Nil, id, delta)
}

func (r *Repository) AdjustWithUser(ctx context.Context, organizationID, userID, id uuid.UUID, delta int64) (InventoryLevel, error) {
	return r.adjust(ctx, organizationID, userID, id, delta)
}

func (r *Repository) adjust(ctx context.Context, organizationID, userID, id uuid.UUID, delta int64) (InventoryLevel, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return InventoryLevel{}, fmt.Errorf("begin stock adjustment: %w", err)
	}
	defer tx.Rollback(ctx)
	var current int64
	var productID, warehouseID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT on_hand_quantity,product_id,warehouse_id FROM inventory_levels WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, id).Scan(&current, &productID, &warehouseID)
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
	var actor any
	if userID != uuid.Nil {
		actor = userID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO inventory_movements (organization_id,inventory_level_id,product_id,warehouse_id,movement_type,quantity_delta,created_by_user_id) VALUES ($1,$2,$3,$4,'adjustment',$5,$6)`, organizationID, id, productID, warehouseID, delta, actor); err != nil {
		return InventoryLevel{}, fmt.Errorf("record inventory adjustment: %w", err)
	}
	var movementID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM inventory_movements WHERE organization_id=$1 AND inventory_level_id=$2 AND product_id=$3 AND warehouse_id=$4 AND movement_type='adjustment' AND quantity_delta=$5 AND created_by_user_id IS NOT DISTINCT FROM $6 ORDER BY created_at DESC, id DESC LIMIT 1`, organizationID, id, productID, warehouseID, delta, actor).Scan(&movementID); err != nil {
		return InventoryLevel{}, fmt.Errorf("load inventory movement id: %w", err)
	}
	actorType := "user"
	var actorUserID *uuid.UUID
	if userID == uuid.Nil {
		actorType = "system"
	} else {
		actorUserID = &userID
	}
	writer := audit.Writer{}
	if err := writer.AppendEvent(ctx, tx, audit.Event{
		OrganizationID: organizationID,
		EventType:      "inventory.adjusted",
		ResourceType:   "inventory_level",
		ResourceID:     id,
		ActorType:      actorType,
		ActorUserID:    actorUserID,
		SourceType:     "inventory_movement",
		SourceID:       movementID,
		Metadata:       map[string]any{"inventory_level_id": id.String(), "quantity_delta": delta},
	}); err != nil {
		return InventoryLevel{}, fmt.Errorf("append inventory.adjusted audit event: %w", err)
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
		FOR UPDATE OF i, p, w`, organizationID, inventoryID).Scan(&onHand, &productActive, &warehouseActive)
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
	currentReservation, err := getReservationTx(ctx, tx, organizationID, reservationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrReservationNotFound
	}
	if err != nil {
		return Reservation{}, fmt.Errorf("read reservation for release: %w", err)
	}
	if currentReservation.Status == "payment_held" || currentReservation.Status == "committed" {
		return Reservation{}, ErrManagedReservation
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
		WHERE organization_id=$1 AND inventory_level_id=$2 AND ((status='active' AND expires_at > statement_timestamp()) OR status IN ('payment_held','committed'))`, organizationID, inventoryID).Scan(&reserved)
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
