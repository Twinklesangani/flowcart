package transfer

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"flowcart/apps/api/internal/audit"
	"flowcart/apps/api/internal/pagination"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const transferColumns = `t.id,t.organization_id,t.source_warehouse_id,t.destination_warehouse_id,
	s.code,d.code,t.status,t.created_by_user_id,t.dispatched_by_user_id,t.received_by_user_id,
	t.cancelled_by_user_id,t.created_at,t.dispatched_at,t.completed_at,t.cancelled_at,
	t.creation_request_hash,COALESCE(t.dispatch_idempotency_key,''),COALESCE(t.receive_idempotency_key,'')`

func scanTransfer(row pgx.Row) (Transfer, error) {
	var item Transfer
	err := row.Scan(&item.ID, &item.OrganizationID, &item.SourceWarehouseID, &item.DestinationWarehouseID,
		&item.SourceWarehouseCode, &item.DestinationWarehouseCode, &item.Status, &item.CreatedByUserID,
		&item.DispatchedByUserID, &item.ReceivedByUserID, &item.CancelledByUserID, &item.CreatedAt,
		&item.DispatchedAt, &item.CompletedAt, &item.CancelledAt, &item.CreationRequestHash,
		&item.DispatchKey, &item.ReceiveKey)
	return item, err
}

func (r *Repository) Create(ctx context.Context, organizationID, userID uuid.UUID, key, requestHash string, input CreateInput) (Transfer, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Transfer{}, fmt.Errorf("begin transfer creation: %w", err)
	}
	defer tx.Rollback(ctx)

	var item Transfer
	err = tx.QueryRow(ctx, `INSERT INTO inventory_transfers
		(organization_id,source_warehouse_id,destination_warehouse_id,created_by_user_id,creation_idempotency_key,creation_request_hash)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (organization_id,creation_idempotency_key) DO NOTHING
		RETURNING id`, organizationID, input.SourceWarehouseID, input.DestinationWarehouseID, userID, key, requestHash).Scan(&item.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		var existingHash string
		if err := tx.QueryRow(ctx, `SELECT creation_request_hash FROM inventory_transfers WHERE organization_id=$1 AND creation_idempotency_key=$2`, organizationID, key).Scan(&existingHash); err != nil {
			return Transfer{}, fmt.Errorf("reload transfer idempotency key: %w", err)
		}
		if existingHash != requestHash {
			return Transfer{}, ErrIdempotencyKeyReused
		}
		var existingID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM inventory_transfers WHERE organization_id=$1 AND creation_idempotency_key=$2`, organizationID, key).Scan(&existingID); err != nil {
			return Transfer{}, fmt.Errorf("reload transfer: %w", err)
		}
		item, err = getTx(ctx, tx, organizationID, existingID)
		if err != nil {
			return Transfer{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Transfer{}, fmt.Errorf("commit transfer replay: %w", err)
		}
		return item, nil
	}
	if err != nil {
		return Transfer{}, fmt.Errorf("insert transfer: %w", err)
	}

	if err := lockWarehouses(ctx, tx, organizationID, input.SourceWarehouseID, input.DestinationWarehouseID); err != nil {
		return Transfer{}, err
	}
	var sourceActive, destinationActive bool
	if err := tx.QueryRow(ctx, `SELECT is_active FROM warehouses WHERE organization_id=$1 AND id=$2`, organizationID, input.SourceWarehouseID).Scan(&sourceActive); err != nil {
		return Transfer{}, fmt.Errorf("read source warehouse: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT is_active FROM warehouses WHERE organization_id=$1 AND id=$2`, organizationID, input.DestinationWarehouseID).Scan(&destinationActive); err != nil {
		return Transfer{}, fmt.Errorf("read destination warehouse: %w", err)
	}
	if !sourceActive || !destinationActive {
		return Transfer{}, ErrInactiveWarehouse
	}

	for _, requested := range input.Items {
		var productActive bool
		if err := tx.QueryRow(ctx, `SELECT is_active FROM products WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, requested.ProductID).Scan(&productActive); errors.Is(err, pgx.ErrNoRows) {
			return Transfer{}, ErrNotFound
		} else if err != nil {
			return Transfer{}, fmt.Errorf("lock transfer product: %w", err)
		}
		if !productActive {
			return Transfer{}, ErrInactiveProduct
		}
		var sourceInventoryID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT i.id FROM inventory_levels i WHERE i.organization_id=$1 AND i.product_id=$2 AND i.warehouse_id=$3 FOR UPDATE`, organizationID, requested.ProductID, input.SourceWarehouseID).Scan(&sourceInventoryID); errors.Is(err, pgx.ErrNoRows) {
			return Transfer{}, ErrSourceInventoryNotFound
		} else if err != nil {
			return Transfer{}, fmt.Errorf("find source inventory: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_levels (organization_id,product_id,warehouse_id,on_hand_quantity) VALUES ($1,$2,$3,0) ON CONFLICT (organization_id,product_id,warehouse_id) DO NOTHING`, organizationID, requested.ProductID, input.DestinationWarehouseID); err != nil {
			return Transfer{}, fmt.Errorf("ensure destination inventory: %w", err)
		}
		var destinationInventoryID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM inventory_levels WHERE organization_id=$1 AND product_id=$2 AND warehouse_id=$3 FOR UPDATE`, organizationID, requested.ProductID, input.DestinationWarehouseID).Scan(&destinationInventoryID); err != nil {
			return Transfer{}, fmt.Errorf("resolve destination inventory: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_transfer_items (organization_id,transfer_id,product_id,source_inventory_level_id,destination_inventory_level_id,quantity) VALUES ($1,$2,$3,$4,$5,$6)`, organizationID, item.ID, requested.ProductID, sourceInventoryID, destinationInventoryID, requested.Quantity); err != nil {
			return Transfer{}, fmt.Errorf("insert transfer item: %w", err)
		}
	}
	item, err = getTx(ctx, tx, organizationID, item.ID)
	if err != nil {
		return Transfer{}, err
	}
	writer := audit.Writer{}
	if err := writer.AppendEvent(ctx, tx, audit.Event{
		OrganizationID: organizationID,
		EventType:      "transfer.created",
		ResourceType:   "transfer",
		ResourceID:     item.ID,
		ActorType:      "user",
		ActorUserID:    &userID,
		SourceType:     "transfer",
		SourceID:       item.ID,
		Metadata:       map[string]any{"source_warehouse_id": input.SourceWarehouseID.String(), "destination_warehouse_id": input.DestinationWarehouseID.String()},
	}); err != nil {
		return Transfer{}, fmt.Errorf("append transfer.created audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Transfer{}, fmt.Errorf("commit transfer creation: %w", err)
	}
	return item, nil
}

func lockWarehouses(ctx context.Context, tx pgx.Tx, organizationID, first, second uuid.UUID) error {
	ids := []uuid.UUID{first, second}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	for _, id := range ids {
		var locked uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM warehouses WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, id).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lock transfer warehouse: %w", err)
		}
	}
	return nil
}

func (r *Repository) List(ctx context.Context, organizationID uuid.UUID, limit int) ([]Transfer, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+transferColumns+` FROM inventory_transfers t JOIN warehouses s ON s.organization_id=t.organization_id AND s.id=t.source_warehouse_id JOIN warehouses d ON d.organization_id=t.organization_id AND d.id=t.destination_warehouse_id WHERE t.organization_id=$1 ORDER BY t.created_at DESC,t.id DESC LIMIT $2`, organizationID, limit)
	if err != nil {
		return nil, fmt.Errorf("list transfers: %w", err)
	}
	defer rows.Close()
	items := make([]Transfer, 0)
	for rows.Next() {
		item, err := scanTransfer(rows)
		if err != nil {
			return nil, fmt.Errorf("scan transfer: %w", err)
		}
		if err := loadItems(ctx, r.pool, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListPage(ctx context.Context, organizationID uuid.UUID, limit int, cursor pagination.Cursor, status string) (ListPage, error) {
	where := "WHERE t.organization_id=$1"
	args := []any{organizationID}
	arg := 2
	if status != "" {
		where += fmt.Sprintf(" AND t.status=$%d", arg)
		args = append(args, status)
		arg++
	}
	if cursor.ID != uuid.Nil {
		where += fmt.Sprintf(" AND (t.created_at,t.id)<($%d,$%d)", arg, arg+1)
		args = append(args, cursor.CreatedAt, cursor.ID)
		arg += 2
	}
	args = append(args, limit+1)
	rows, err := r.pool.Query(ctx, `SELECT `+transferColumns+` FROM inventory_transfers t JOIN warehouses s ON s.organization_id=t.organization_id AND s.id=t.source_warehouse_id JOIN warehouses d ON d.organization_id=t.organization_id AND d.id=t.destination_warehouse_id `+where+fmt.Sprintf(` ORDER BY t.created_at DESC,t.id DESC LIMIT $%d`, arg), args...)
	if err != nil {
		return ListPage{}, fmt.Errorf("list transfers: %w", err)
	}
	defer rows.Close()
	items := make([]Transfer, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanTransfer(rows)
		if scanErr != nil {
			return ListPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListPage{}, err
	}
	rows.Close()
	page := ListPage{Transfers: items}
	if len(items) > limit {
		page.Transfers = items[:limit]
		page.HasMore = true
		last := page.Transfers[len(page.Transfers)-1]
		page.NextCursor = pagination.EncodeCursor(pagination.Cursor{OrganizationID: organizationID, CreatedAt: last.CreatedAt, ID: last.ID})
	}
	for index := range page.Transfers {
		if err := loadItems(ctx, r.pool, &page.Transfers[index]); err != nil {
			return ListPage{}, err
		}
	}
	return page, nil
}

func (r *Repository) Get(ctx context.Context, organizationID, id uuid.UUID) (Transfer, error) {
	return getTransfer(ctx, r.pool, organizationID, id)
}

func (r *Repository) Timeline(ctx context.Context, organizationID, id uuid.UUID) ([]audit.Event, error) {
	return audit.NewReader(r.pool).TransferTimeline(ctx, organizationID, id)
}

func getTransfer(ctx context.Context, q queryer, organizationID, id uuid.UUID) (Transfer, error) {
	item, err := scanTransfer(q.QueryRow(ctx, `SELECT `+transferColumns+` FROM inventory_transfers t JOIN warehouses s ON s.organization_id=t.organization_id AND s.id=t.source_warehouse_id JOIN warehouses d ON d.organization_id=t.organization_id AND d.id=t.destination_warehouse_id WHERE t.organization_id=$1 AND t.id=$2`, organizationID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Transfer{}, ErrNotFound
	}
	if err != nil {
		return Transfer{}, fmt.Errorf("get transfer: %w", err)
	}
	if err := loadItems(ctx, q, &item); err != nil {
		return Transfer{}, err
	}
	return item, nil
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getTx(ctx context.Context, tx pgx.Tx, organizationID, id uuid.UUID) (Transfer, error) {
	return getTransfer(ctx, tx, organizationID, id)
}

func loadItems(ctx context.Context, q queryer, transfer *Transfer) error {
	rows, err := q.Query(ctx, `SELECT i.id,i.organization_id,i.transfer_id,i.product_id,p.sku,p.name,i.source_inventory_level_id,i.destination_inventory_level_id,i.quantity,i.created_at FROM inventory_transfer_items i JOIN products p ON p.organization_id=i.organization_id AND p.id=i.product_id WHERE i.organization_id=$1 AND i.transfer_id=$2 ORDER BY i.id`, transfer.OrganizationID, transfer.ID)
	if err != nil {
		return fmt.Errorf("load transfer items: %w", err)
	}
	defer rows.Close()
	transfer.Items = []TransferItem{}
	for rows.Next() {
		var item TransferItem
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.TransferID, &item.ProductID, &item.ProductSKU, &item.ProductName, &item.SourceInventoryLevelID, &item.DestinationInventoryLevelID, &item.Quantity, &item.CreatedAt); err != nil {
			return err
		}
		transfer.Items = append(transfer.Items, item)
	}
	return rows.Err()
}

func lockInventoryIDs(ctx context.Context, tx pgx.Tx, organizationID uuid.UUID, ids []uuid.UUID) error {
	unique := make(map[uuid.UUID]struct{}, len(ids))
	ordered := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := unique[id]; ok {
			continue
		}
		unique[id] = struct{}{}
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	for _, id := range ordered {
		var locked uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM inventory_levels WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, id).Scan(&locked); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lock transfer inventory: %w", err)
		}
	}
	return nil
}

func transferItemIDs(items []TransferItem) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(items)*2)
	for _, item := range items {
		ids = append(ids, item.SourceInventoryLevelID, item.DestinationInventoryLevelID)
	}
	return ids
}

func (r *Repository) Dispatch(ctx context.Context, organizationID, userID, id uuid.UUID, key string) (Transfer, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Transfer{}, fmt.Errorf("begin transfer dispatch: %w", err)
	}
	defer tx.Rollback(ctx)
	initial, err := getTx(ctx, tx, organizationID, id)
	if err != nil {
		return Transfer{}, err
	}
	if err := lockInventoryIDs(ctx, tx, organizationID, transferItemIDs(initial.Items)); err != nil {
		return Transfer{}, err
	}
	var item Transfer
	var lockErr error
	item, lockErr = scanTransfer(tx.QueryRow(ctx, `SELECT `+transferColumns+` FROM inventory_transfers t JOIN warehouses s ON s.organization_id=t.organization_id AND s.id=t.source_warehouse_id JOIN warehouses d ON d.organization_id=t.organization_id AND d.id=t.destination_warehouse_id WHERE t.organization_id=$1 AND t.id=$2 FOR UPDATE`, organizationID, id))
	if lockErr != nil {
		return Transfer{}, fmt.Errorf("lock transfer dispatch: %w", lockErr)
	}
	if item.Status == StatusInTransit && item.DispatchedByUserID != nil && item.DispatchKey == key {
		return getTx(ctx, tx, organizationID, id)
	}
	if item.Status != StatusPending {
		if item.Status == StatusInTransit {
			return Transfer{}, ErrTransferNotPending
		}
		return Transfer{}, ErrTransferNotPending
	}
	if item.DispatchKey != "" && item.DispatchKey != key {
		return Transfer{}, ErrTransferNotPending
	}
	items, err := transferItemsTx(ctx, tx, organizationID, id)
	if err != nil {
		return Transfer{}, err
	}
	if err := validateDispatchState(ctx, tx, organizationID, item, items); err != nil {
		return Transfer{}, err
	}
	for _, transferItem := range items {
		if err := expireReservationsTx(ctx, tx, organizationID, transferItem.SourceInventoryLevelID); err != nil {
			return Transfer{}, err
		}
		var onHand, reserved int64
		if err := tx.QueryRow(ctx, `SELECT on_hand_quantity FROM inventory_levels WHERE organization_id=$1 AND id=$2`, organizationID, transferItem.SourceInventoryLevelID).Scan(&onHand); err != nil {
			return Transfer{}, err
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(quantity) FILTER (WHERE ((status='active' AND expires_at > statement_timestamp()) OR status IN ('payment_held','committed'))),0) FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2`, organizationID, transferItem.SourceInventoryLevelID).Scan(&reserved); err != nil {
			return Transfer{}, err
		}
		if onHand-reserved < transferItem.Quantity {
			return Transfer{}, ErrInsufficientTransferableStock
		}
	}
	for _, transferItem := range items {
		if _, err := tx.Exec(ctx, `UPDATE inventory_levels SET on_hand_quantity=on_hand_quantity-$3,updated_at=NOW() WHERE organization_id=$1 AND id=$2`, organizationID, transferItem.SourceInventoryLevelID, transferItem.Quantity); err != nil {
			return Transfer{}, fmt.Errorf("deduct transfer source: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_movements (organization_id,inventory_level_id,product_id,warehouse_id,movement_type,quantity_delta,transfer_id,transfer_item_id,created_by_user_id) VALUES ($1,$2,$3,$4,'transfer_out',$5,$6,$7,$8)`, organizationID, transferItem.SourceInventoryLevelID, transferItem.ProductID, item.SourceWarehouseID, -transferItem.Quantity, id, transferItem.ID, userID); err != nil {
			return Transfer{}, fmt.Errorf("record transfer out: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE inventory_transfers SET status='in_transit',dispatched_by_user_id=$3,dispatched_at=NOW(),dispatch_idempotency_key=$4 WHERE organization_id=$1 AND id=$2 AND status='pending'`, organizationID, id, userID, key); err != nil {
		return Transfer{}, fmt.Errorf("mark transfer in transit: %w", err)
	}
	item, err = getTx(ctx, tx, organizationID, id)
	if err != nil {
		return Transfer{}, err
	}
	writer := audit.Writer{}
	if err := writer.AppendEvent(ctx, tx, audit.Event{
		OrganizationID: organizationID,
		EventType:      "transfer.dispatched",
		ResourceType:   "transfer",
		ResourceID:     id,
		ActorType:      "user",
		ActorUserID:    &userID,
		SourceType:     "transfer",
		SourceID:       id,
		Metadata:       map[string]any{"status": "in_transit"},
	}); err != nil {
		return Transfer{}, fmt.Errorf("append transfer.dispatched audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Transfer{}, fmt.Errorf("commit transfer dispatch: %w", err)
	}
	return item, nil
}

func (r *Repository) Receive(ctx context.Context, organizationID, userID, id uuid.UUID, key string) (Transfer, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Transfer{}, fmt.Errorf("begin transfer receive: %w", err)
	}
	defer tx.Rollback(ctx)
	initial, err := getTx(ctx, tx, organizationID, id)
	if err != nil {
		return Transfer{}, err
	}
	ids := make([]uuid.UUID, 0, len(initial.Items))
	for _, item := range initial.Items {
		ids = append(ids, item.DestinationInventoryLevelID)
	}
	if err := lockInventoryIDs(ctx, tx, organizationID, ids); err != nil {
		return Transfer{}, err
	}
	var item Transfer
	item, err = scanTransfer(tx.QueryRow(ctx, `SELECT `+transferColumns+` FROM inventory_transfers t JOIN warehouses s ON s.organization_id=t.organization_id AND s.id=t.source_warehouse_id JOIN warehouses d ON d.organization_id=t.organization_id AND d.id=t.destination_warehouse_id WHERE t.organization_id=$1 AND t.id=$2 FOR UPDATE`, organizationID, id))
	if err != nil {
		return Transfer{}, err
	}
	if item.Status == StatusCompleted {
		return getTx(ctx, tx, organizationID, id)
	}
	if item.Status != StatusInTransit {
		return Transfer{}, ErrTransferNotInTransit
	}
	items, err := transferItemsTx(ctx, tx, organizationID, id)
	if err != nil {
		return Transfer{}, err
	}
	for _, transferItem := range items {
		if _, err := tx.Exec(ctx, `UPDATE inventory_levels SET on_hand_quantity=on_hand_quantity+$3,updated_at=NOW() WHERE organization_id=$1 AND id=$2`, organizationID, transferItem.DestinationInventoryLevelID, transferItem.Quantity); err != nil {
			return Transfer{}, fmt.Errorf("add transfer destination: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_movements (organization_id,inventory_level_id,product_id,warehouse_id,movement_type,quantity_delta,transfer_id,transfer_item_id,created_by_user_id) VALUES ($1,$2,$3,$4,'transfer_in',$5,$6,$7,$8)`, organizationID, transferItem.DestinationInventoryLevelID, transferItem.ProductID, item.DestinationWarehouseID, transferItem.Quantity, id, transferItem.ID, userID); err != nil {
			return Transfer{}, fmt.Errorf("record transfer in: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE inventory_transfers SET status='completed',received_by_user_id=$3,completed_at=NOW(),receive_idempotency_key=$4 WHERE organization_id=$1 AND id=$2 AND status='in_transit'`, organizationID, id, userID, key); err != nil {
		return Transfer{}, fmt.Errorf("complete transfer: %w", err)
	}
	item, err = getTx(ctx, tx, organizationID, id)
	if err != nil {
		return Transfer{}, err
	}
	writer := audit.Writer{}
	if err := writer.AppendEvent(ctx, tx, audit.Event{
		OrganizationID: organizationID,
		EventType:      "transfer.received",
		ResourceType:   "transfer",
		ResourceID:     id,
		ActorType:      "user",
		ActorUserID:    &userID,
		SourceType:     "transfer",
		SourceID:       id,
		Metadata:       map[string]any{"status": "completed"},
	}); err != nil {
		return Transfer{}, fmt.Errorf("append transfer.received audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Transfer{}, fmt.Errorf("commit transfer receive: %w", err)
	}
	return item, nil
}

func transferItemsTx(ctx context.Context, tx pgx.Tx, organizationID, transferID uuid.UUID) ([]TransferItem, error) {
	var transfer Transfer
	transfer.OrganizationID = organizationID
	transfer.ID = transferID
	if err := loadItems(ctx, tx, &transfer); err != nil {
		return nil, err
	}
	return transfer.Items, nil
}

func validateDispatchState(ctx context.Context, tx pgx.Tx, organizationID uuid.UUID, item Transfer, items []TransferItem) error {
	var sourceActive, destinationActive bool
	if err := tx.QueryRow(ctx, `SELECT is_active FROM warehouses WHERE organization_id=$1 AND id=$2`, organizationID, item.SourceWarehouseID).Scan(&sourceActive); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT is_active FROM warehouses WHERE organization_id=$1 AND id=$2`, organizationID, item.DestinationWarehouseID).Scan(&destinationActive); err != nil {
		return err
	}
	if !sourceActive || !destinationActive {
		return ErrInactiveWarehouse
	}
	for _, transferItem := range items {
		var active bool
		if err := tx.QueryRow(ctx, `SELECT is_active FROM products WHERE organization_id=$1 AND id=$2`, organizationID, transferItem.ProductID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return ErrInactiveProduct
		}
	}
	return nil
}

func (r *Repository) Cancel(ctx context.Context, organizationID, userID, id uuid.UUID) (Transfer, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Transfer{}, fmt.Errorf("begin transfer cancellation: %w", err)
	}
	defer tx.Rollback(ctx)
	var item Transfer
	item, err = scanTransfer(tx.QueryRow(ctx, `SELECT `+transferColumns+` FROM inventory_transfers t JOIN warehouses s ON s.organization_id=t.organization_id AND s.id=t.source_warehouse_id JOIN warehouses d ON d.organization_id=t.organization_id AND d.id=t.destination_warehouse_id WHERE t.organization_id=$1 AND t.id=$2 FOR UPDATE`, organizationID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Transfer{}, ErrNotFound
		}
		return Transfer{}, err
	}
	if item.Status != StatusPending {
		return Transfer{}, ErrTransferNotPending
	}
	if _, err := tx.Exec(ctx, `UPDATE inventory_transfers SET status='cancelled',cancelled_by_user_id=$3,cancelled_at=NOW() WHERE organization_id=$1 AND id=$2 AND status='pending'`, organizationID, id, userID); err != nil {
		return Transfer{}, fmt.Errorf("cancel transfer: %w", err)
	}
	writer := audit.Writer{}
	if err := writer.AppendEvent(ctx, tx, audit.Event{
		OrganizationID: organizationID,
		EventType:      "transfer.cancelled",
		ResourceType:   "transfer",
		ResourceID:     id,
		ActorType:      "user",
		ActorUserID:    &userID,
		SourceType:     "transfer",
		SourceID:       id,
		Metadata:       map[string]any{"status": "cancelled"},
	}); err != nil {
		return Transfer{}, fmt.Errorf("append transfer.cancelled audit event: %w", err)
	}
	item, err = getTx(ctx, tx, organizationID, id)
	if err != nil {
		return Transfer{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Transfer{}, fmt.Errorf("commit transfer cancellation: %w", err)
	}
	return item, nil
}

func expireReservationsTx(ctx context.Context, tx pgx.Tx, organizationID, inventoryID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE inventory_reservations SET status='expired',expired_at=NOW() WHERE organization_id=$1 AND inventory_level_id=$2 AND status='active' AND expires_at<=NOW()`, organizationID, inventoryID)
	return err
}
