package fulfillment

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"flowcart/apps/api/internal/audit"
	"flowcart/apps/api/internal/organization"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

type reservationRecord struct {
	ID, OrderItemID, InventoryLevelID, OrderID, WarehouseID, ProductID uuid.UUID
	Quantity                                                           int64
	Status                                                             string
}

const fulfillmentColumns = `f.id, f.organization_id, f.order_id, f.warehouse_id, w.code, w.name,
	 f.created_by_user_id, f.status, f.idempotency_key, f.request_hash, f.created_at, f.completed_at`

func scanFulfillment(row pgx.Row) (Fulfillment, error) {
	var item Fulfillment
	err := row.Scan(&item.ID, &item.OrganizationID, &item.OrderID, &item.WarehouseID,
		&item.WarehouseCode, &item.WarehouseName, &item.CreatedByUserID, &item.Status,
		&item.IdempotencyKey, &item.RequestHash, &item.CreatedAt, &item.CompletedAt)
	return item, err
}

func (r *Repository) Complete(ctx context.Context, tenant organization.TenantContext, orderID uuid.UUID, key, requestHash string, reservationIDs []uuid.UUID) (Fulfillment, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Fulfillment{}, fmt.Errorf("begin fulfillment: %w", err)
	}
	defer tx.Rollback(ctx)
	initial, err := scanFulfillment(tx.QueryRow(ctx, `SELECT `+fulfillmentColumns+` FROM fulfillments f JOIN warehouses w ON w.organization_id=f.organization_id AND w.id=f.warehouse_id WHERE f.organization_id=$1 AND f.idempotency_key=$2 FOR UPDATE`, tenant.OrganizationID, key))
	if err == nil {
		if initial.OrderID != orderID || initial.requestHash() != requestHash {
			return Fulfillment{}, ErrIdempotencyKeyReused
		}
		initial, err = loadFulfillmentTx(ctx, tx, initial)
		if err != nil {
			return Fulfillment{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Fulfillment{}, fmt.Errorf("commit early fulfillment replay: %w", err)
		}
		return initial, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Fulfillment{}, fmt.Errorf("find early fulfillment idempotency key: %w", err)
	}

	records, err := discoverReservations(ctx, tx, tenant.OrganizationID, reservationIDs)
	if err != nil {
		return Fulfillment{}, err
	}
	if len(records) != len(reservationIDs) {
		return Fulfillment{}, ErrReservationNotFound
	}
	warehouseID := records[0].WarehouseID
	for _, record := range records {
		if record.OrderID != orderID {
			return Fulfillment{}, ErrReservationOtherOrder
		}
		if record.WarehouseID != warehouseID {
			return Fulfillment{}, ErrMixedWarehouse
		}
	}

	inventoryIDs := make([]uuid.UUID, 0, len(records))
	seenInventory := make(map[uuid.UUID]bool, len(records))
	for _, record := range records {
		if !seenInventory[record.InventoryLevelID] {
			seenInventory[record.InventoryLevelID] = true
			inventoryIDs = append(inventoryIDs, record.InventoryLevelID)
		}
	}
	sort.Slice(inventoryIDs, func(i, j int) bool { return inventoryIDs[i].String() < inventoryIDs[j].String() })
	for _, inventoryID := range inventoryIDs {
		var locked uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM inventory_levels WHERE organization_id=$1 AND id=$2 FOR UPDATE`, tenant.OrganizationID, inventoryID).Scan(&locked); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Fulfillment{}, ErrReservationNotFound
			}
			return Fulfillment{}, fmt.Errorf("lock fulfillment inventory: %w", err)
		}
	}

	var orderStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM orders WHERE organization_id=$1 AND id=$2 FOR UPDATE`, tenant.OrganizationID, orderID).Scan(&orderStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Fulfillment{}, ErrOrderNotFound
		}
		return Fulfillment{}, fmt.Errorf("lock fulfillment order: %w", err)
	}
	existing, err := scanFulfillment(tx.QueryRow(ctx, `SELECT `+fulfillmentColumns+` FROM fulfillments f JOIN warehouses w ON w.organization_id=f.organization_id AND w.id=f.warehouse_id WHERE f.organization_id=$1 AND f.idempotency_key=$2 FOR UPDATE`, tenant.OrganizationID, key))
	if err == nil {
		if existing.OrderID != orderID || existing.requestHash() != requestHash {
			return Fulfillment{}, ErrIdempotencyKeyReused
		}
		existing, err = loadFulfillmentTx(ctx, tx, existing)
		if err != nil {
			return Fulfillment{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Fulfillment{}, fmt.Errorf("commit fulfillment replay: %w", err)
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Fulfillment{}, fmt.Errorf("find fulfillment idempotency key: %w", err)
	}
	if orderStatus == "fulfilled" {
		return Fulfillment{}, ErrOrderAlreadyFulfilled
	}
	if orderStatus != "paid" && orderStatus != "partially_fulfilled" {
		return Fulfillment{}, ErrOrderNotPaid
	}

	lockedRecords, err := lockReservations(ctx, tx, tenant.OrganizationID, reservationIDs)
	if err != nil {
		return Fulfillment{}, err
	}
	if len(lockedRecords) != len(reservationIDs) {
		return Fulfillment{}, ErrReservationNotFound
	}
	quantities := make(map[uuid.UUID]int64, len(inventoryIDs))
	for _, record := range lockedRecords {
		if record.OrderID != orderID || record.WarehouseID != warehouseID {
			return Fulfillment{}, ErrReservationOtherOrder
		}
		if record.Status == "consumed" {
			return Fulfillment{}, ErrReservationAlreadyConsumed
		}
		if record.Status != "committed" {
			return Fulfillment{}, ErrReservationNotCommitted
		}
		if quantities[record.InventoryLevelID] > 0 && quantities[record.InventoryLevelID] > int64(^uint64(0)>>1)-record.Quantity {
			return Fulfillment{}, ErrInsufficientStock
		}
		quantities[record.InventoryLevelID] += record.Quantity
	}

	var fulfillmentID uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO fulfillments (organization_id,order_id,warehouse_id,created_by_user_id,status,idempotency_key,request_hash) VALUES ($1,$2,$3,$4,'completed',$5,$6) ON CONFLICT (organization_id,idempotency_key) DO NOTHING RETURNING id`, tenant.OrganizationID, orderID, warehouseID, tenant.UserID, key, requestHash).Scan(&fulfillmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, loadErr := scanFulfillment(tx.QueryRow(ctx, `SELECT `+fulfillmentColumns+` FROM fulfillments f JOIN warehouses w ON w.organization_id=f.organization_id AND w.id=f.warehouse_id WHERE f.organization_id=$1 AND f.idempotency_key=$2 FOR UPDATE`, tenant.OrganizationID, key))
		if loadErr != nil {
			return Fulfillment{}, fmt.Errorf("reload fulfillment idempotency key: %w", loadErr)
		}
		if existing.OrderID != orderID || existing.requestHash() != requestHash {
			return Fulfillment{}, ErrIdempotencyKeyReused
		}
		existing, loadErr = loadFulfillmentTx(ctx, tx, existing)
		if loadErr != nil {
			return Fulfillment{}, loadErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Fulfillment{}, fmt.Errorf("commit concurrent fulfillment replay: %w", err)
		}
		return existing, nil
	}
	if err != nil {
		return Fulfillment{}, fmt.Errorf("create fulfillment: %w", err)
	}
	fulfillment, err := scanFulfillment(tx.QueryRow(ctx, `SELECT `+fulfillmentColumns+` FROM fulfillments f JOIN warehouses w ON w.organization_id=f.organization_id AND w.id=f.warehouse_id WHERE f.organization_id=$1 AND f.id=$2`, tenant.OrganizationID, fulfillmentID))
	if err != nil {
		return Fulfillment{}, fmt.Errorf("load created fulfillment: %w", err)
	}

	for _, inventoryID := range inventoryIDs {
		var onHand int64
		if err := tx.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE organization_id=$1 AND id=$2 FOR UPDATE`, tenant.OrganizationID, inventoryID).Scan(&onHand); err != nil {
			return Fulfillment{}, fmt.Errorf("read fulfillment stock: %w", err)
		}
		if onHand < quantities[inventoryID] {
			return Fulfillment{}, ErrInsufficientStock
		}
		if _, err := tx.Exec(ctx, `UPDATE inventory_levels SET on_hand_quantity=on_hand_quantity-$3,updated_at=NOW() WHERE organization_id=$1 AND id=$2`, tenant.OrganizationID, inventoryID, quantities[inventoryID]); err != nil {
			return Fulfillment{}, fmt.Errorf("deduct fulfillment stock: %w", err)
		}
	}
	for _, record := range lockedRecords {
		if _, err := tx.Exec(ctx, `INSERT INTO fulfillment_items (organization_id,fulfillment_id,order_id,order_item_id,reservation_id,inventory_level_id,warehouse_id,quantity) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, tenant.OrganizationID, fulfillment.ID, orderID, record.OrderItemID, record.ID, record.InventoryLevelID, record.WarehouseID, record.Quantity); err != nil {
			return Fulfillment{}, fmt.Errorf("create fulfillment item: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_movements (organization_id,inventory_level_id,product_id,warehouse_id,movement_type,quantity_delta,fulfillment_id,reservation_id,created_by_user_id) VALUES ($1,$2,$3,$4,'fulfillment',$5,$6,$7,$8)`, tenant.OrganizationID, record.InventoryLevelID, record.ProductID, record.WarehouseID, -record.Quantity, fulfillment.ID, record.ID, tenant.UserID); err != nil {
			return Fulfillment{}, fmt.Errorf("create fulfillment movement: %w", err)
		}
		command, err := tx.Exec(ctx, `UPDATE inventory_reservations SET status='consumed' WHERE organization_id=$1 AND id=$2 AND status='committed'`, tenant.OrganizationID, record.ID)
		if err != nil {
			return Fulfillment{}, fmt.Errorf("consume fulfillment reservation: %w", err)
		}
		if command.RowsAffected() != 1 {
			return Fulfillment{}, ErrReservationNotCommitted
		}
	}

	var allConsumed bool
	if err := tx.QueryRow(ctx, `SELECT COALESCE(bool_and(consumed_quantity >= required_quantity),false) FROM (SELECT oi.quantity AS required_quantity,COALESCE(SUM(r.quantity) FILTER (WHERE r.status='consumed'),0) AS consumed_quantity FROM order_items oi LEFT JOIN inventory_reservations r ON r.organization_id=oi.organization_id AND r.order_item_id=oi.id WHERE oi.organization_id=$1 AND oi.order_id=$2 GROUP BY oi.id,oi.quantity) fulfillment_coverage`, tenant.OrganizationID, orderID).Scan(&allConsumed); err != nil {
		return Fulfillment{}, fmt.Errorf("derive order fulfillment state: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET status=CASE WHEN $3 THEN 'fulfilled' ELSE 'partially_fulfilled' END,fulfilled_at=CASE WHEN $3 THEN COALESCE(fulfilled_at,NOW()) ELSE fulfilled_at END,updated_at=NOW() WHERE organization_id=$1 AND id=$2 AND status IN ('paid','partially_fulfilled')`, tenant.OrganizationID, orderID, allConsumed); err != nil {
		return Fulfillment{}, fmt.Errorf("update order fulfillment state: %w", err)
	}
	writer := audit.Writer{}
	if err := writer.AppendEvent(ctx, tx, audit.Event{
		OrganizationID: tenant.OrganizationID,
		EventType:      "fulfillment.completed",
		ResourceType:   "fulfillment",
		ResourceID:     fulfillment.ID,
		ActorType:      "system",
		SourceType:     "fulfillment",
		SourceID:       fulfillment.ID,
		Metadata:       map[string]any{"order_id": orderID.String(), "warehouse_id": warehouseID.String(), "items": len(lockedRecords)},
	}); err != nil {
		return Fulfillment{}, fmt.Errorf("append fulfillment.completed audit event: %w", err)
	}
	if allConsumed {
		if err := writer.AppendEvent(ctx, tx, audit.Event{
			OrganizationID: tenant.OrganizationID,
			EventType:      "order.fulfilled",
			ResourceType:   "order",
			ResourceID:     orderID,
			ActorType:      "system",
			SourceType:     "fulfillment",
			SourceID:       fulfillment.ID,
			Metadata:       map[string]any{"fulfillment_id": fulfillment.ID.String(), "status": "fulfilled"},
		}); err != nil {
			return Fulfillment{}, fmt.Errorf("append order.fulfilled audit event: %w", err)
		}
	} else {
		if err := writer.AppendEvent(ctx, tx, audit.Event{
			OrganizationID: tenant.OrganizationID,
			EventType:      "order.partially_fulfilled",
			ResourceType:   "order",
			ResourceID:     orderID,
			ActorType:      "system",
			SourceType:     "fulfillment",
			SourceID:       fulfillment.ID,
			Metadata:       map[string]any{"fulfillment_id": fulfillment.ID.String(), "status": "partially_fulfilled"},
		}); err != nil {
			return Fulfillment{}, fmt.Errorf("append order.partially_fulfilled audit event: %w", err)
		}
	}
	fulfillment, err = loadFulfillmentTx(ctx, tx, fulfillment)
	if err != nil {
		return Fulfillment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Fulfillment{}, fmt.Errorf("commit fulfillment: %w", err)
	}
	return fulfillment, nil
}

func (f Fulfillment) requestHash() string { return f.RequestHash }

func discoverReservations(ctx context.Context, tx pgx.Tx, organizationID uuid.UUID, ids []uuid.UUID) ([]reservationRecord, error) {
	rows, err := tx.Query(ctx, `SELECT r.id,r.order_item_id,r.inventory_level_id,oi.order_id,i.warehouse_id,i.product_id,r.quantity,r.status FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id JOIN inventory_levels i ON i.organization_id=r.organization_id AND i.id=r.inventory_level_id WHERE r.organization_id=$1 AND r.id=ANY($2)`, organizationID, ids)
	if err != nil {
		return nil, fmt.Errorf("discover fulfillment reservations: %w", err)
	}
	defer rows.Close()
	return scanReservations(rows)
}

func lockReservations(ctx context.Context, tx pgx.Tx, organizationID uuid.UUID, ids []uuid.UUID) ([]reservationRecord, error) {
	rows, err := tx.Query(ctx, `SELECT r.id,r.order_item_id,r.inventory_level_id,oi.order_id,i.warehouse_id,i.product_id,r.quantity,r.status FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id JOIN inventory_levels i ON i.organization_id=r.organization_id AND i.id=r.inventory_level_id WHERE r.organization_id=$1 AND r.id=ANY($2) ORDER BY r.id FOR UPDATE OF r`, organizationID, ids)
	if err != nil {
		return nil, fmt.Errorf("lock fulfillment reservations: %w", err)
	}
	defer rows.Close()
	return scanReservations(rows)
}

func scanReservations(rows pgx.Rows) ([]reservationRecord, error) {
	records := []reservationRecord{}
	for rows.Next() {
		var record reservationRecord
		if err := rows.Scan(&record.ID, &record.OrderItemID, &record.InventoryLevelID, &record.OrderID, &record.WarehouseID, &record.ProductID, &record.Quantity, &record.Status); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func loadFulfillmentTx(ctx context.Context, tx pgx.Tx, item Fulfillment) (Fulfillment, error) {
	rows, err := tx.Query(ctx, `SELECT id,organization_id,fulfillment_id,order_id,order_item_id,reservation_id,inventory_level_id,warehouse_id,quantity FROM fulfillment_items WHERE organization_id=$1 AND fulfillment_id=$2 ORDER BY id`, item.OrganizationID, item.ID)
	if err != nil {
		return Fulfillment{}, fmt.Errorf("load fulfillment items: %w", err)
	}
	defer rows.Close()
	item.Items = []FulfillmentItem{}
	for rows.Next() {
		var child FulfillmentItem
		if err := rows.Scan(&child.ID, &child.OrganizationID, &child.FulfillmentID, &child.OrderID, &child.OrderItemID, &child.ReservationID, &child.InventoryLevelID, &child.WarehouseID, &child.Quantity); err != nil {
			return Fulfillment{}, err
		}
		item.Items = append(item.Items, child)
	}
	if err := rows.Err(); err != nil {
		return Fulfillment{}, err
	}
	return item, nil
}

func (r *Repository) List(ctx context.Context, organizationID, orderID uuid.UUID) ([]Fulfillment, error) {
	var existingOrder uuid.UUID
	if err := r.pool.QueryRow(ctx, `SELECT id FROM orders WHERE organization_id=$1 AND id=$2`, organizationID, orderID).Scan(&existingOrder); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOrderNotFound
	} else if err != nil {
		return nil, fmt.Errorf("find fulfillment order: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT `+fulfillmentColumns+` FROM fulfillments f JOIN warehouses w ON w.organization_id=f.organization_id AND w.id=f.warehouse_id WHERE f.organization_id=$1 AND f.order_id=$2 ORDER BY f.created_at,f.id`, organizationID, orderID)
	if err != nil {
		return nil, fmt.Errorf("list fulfillments: %w", err)
	}
	defer rows.Close()
	items := []Fulfillment{}
	for rows.Next() {
		item, scanErr := scanFulfillment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return r.loadFulfillmentItems(ctx, items)
}

func (r *Repository) Get(ctx context.Context, organizationID, id uuid.UUID) (Fulfillment, error) {
	item, err := scanFulfillment(r.pool.QueryRow(ctx, `SELECT `+fulfillmentColumns+` FROM fulfillments f JOIN warehouses w ON w.organization_id=f.organization_id AND w.id=f.warehouse_id WHERE f.organization_id=$1 AND f.id=$2`, organizationID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Fulfillment{}, ErrNotFound
	}
	if err != nil {
		return Fulfillment{}, fmt.Errorf("get fulfillment: %w", err)
	}
	items, err := r.loadFulfillmentItems(ctx, []Fulfillment{item})
	if err != nil {
		return Fulfillment{}, err
	}
	return items[0], nil
}

func (r *Repository) loadFulfillmentItems(ctx context.Context, items []Fulfillment) ([]Fulfillment, error) {
	if len(items) == 0 {
		return items, nil
	}
	ids := make([]uuid.UUID, len(items))
	byID := make(map[uuid.UUID]int, len(items))
	for i := range items {
		ids[i] = items[i].ID
		byID[items[i].ID] = i
		items[i].Items = []FulfillmentItem{}
	}
	rows, err := r.pool.Query(ctx, `SELECT id,organization_id,fulfillment_id,order_id,order_item_id,reservation_id,inventory_level_id,warehouse_id,quantity FROM fulfillment_items WHERE organization_id=$1 AND fulfillment_id=ANY($2) ORDER BY id`, items[0].OrganizationID, ids)
	if err != nil {
		return nil, fmt.Errorf("list fulfillment items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var child FulfillmentItem
		if err := rows.Scan(&child.ID, &child.OrganizationID, &child.FulfillmentID, &child.OrderID, &child.OrderItemID, &child.ReservationID, &child.InventoryLevelID, &child.WarehouseID, &child.Quantity); err != nil {
			return nil, err
		}
		items[byID[child.FulfillmentID]].Items = append(items[byID[child.FulfillmentID]].Items, child)
	}
	return items, rows.Err()
}

func (r *Repository) ListMovements(ctx context.Context, organizationID, inventoryID uuid.UUID, limit int) ([]InventoryMovement, error) {
	var existingInventory uuid.UUID
	if err := r.pool.QueryRow(ctx, `SELECT id FROM inventory_levels WHERE organization_id=$1 AND id=$2`, organizationID, inventoryID).Scan(&existingInventory); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("find movement inventory: %w", err)
	}
	rows, err := r.pool.Query(ctx, `SELECT id,organization_id,inventory_level_id,product_id,warehouse_id,movement_type,quantity_delta,fulfillment_id,reservation_id,created_by_user_id,created_at FROM inventory_movements WHERE organization_id=$1 AND inventory_level_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3`, organizationID, inventoryID, limit)
	if err != nil {
		return nil, fmt.Errorf("list inventory movements: %w", err)
	}
	defer rows.Close()
	items := []InventoryMovement{}
	for rows.Next() {
		var item InventoryMovement
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.InventoryLevelID, &item.ProductID, &item.WarehouseID, &item.MovementType, &item.QuantityDelta, &item.FulfillmentID, &item.ReservationID, &item.CreatedByUserID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
