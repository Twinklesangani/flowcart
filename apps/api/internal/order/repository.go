package order

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"flowcart/apps/api/internal/audit"
	"flowcart/apps/api/internal/inventory"
	"flowcart/apps/api/internal/pagination"
	"flowcart/apps/api/internal/payment"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

type pricingSnapshot struct {
	productID uuid.UUID
	sku       string
	name      string
	price     *int64
	currency  *string
}

func productCurrencyAllowed(value *string) bool {
	return value != nil && (*value == "AUD" || *value == "USD" || *value == "INR")
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const orderColumns = `id, organization_id, created_by_user_id, status, idempotency_key, request_hash, allocation_method, allocation_strategy, created_at, updated_at, cancelled_at, paid_at, cancel_requested_at, currency_code, subtotal_minor`

func scanOrder(row pgx.Row) (Order, error) {
	var item Order
	err := row.Scan(&item.ID, &item.OrganizationID, &item.CreatedByUserID, &item.Status, &item.IdempotencyKey, &item.RequestHash, &item.AllocationMethod, &item.AllocationStrategy, &item.CreatedAt, &item.UpdatedAt, &item.CancelledAt, &item.PaidAt, &item.CancelRequestedAt, &item.CurrencyCode, &item.SubtotalMinor)
	return item, err
}

func (r *Repository) Create(ctx context.Context, organizationID, userID uuid.UUID, key, requestHash string, input CreateInput) (Order, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("begin order: %w", err)
	}
	defer tx.Rollback(ctx)
	var item Order
	err = tx.QueryRow(ctx, `INSERT INTO orders (organization_id, created_by_user_id, idempotency_key, request_hash, allocation_method)
			VALUES ($1,$2,$3,$4,'manual') ON CONFLICT (organization_id,idempotency_key) DO NOTHING RETURNING `+orderColumns, organizationID, userID, key, requestHash).Scan(&item.ID, &item.OrganizationID, &item.CreatedByUserID, &item.Status, &item.IdempotencyKey, &item.RequestHash, &item.AllocationMethod, &item.AllocationStrategy, &item.CreatedAt, &item.UpdatedAt, &item.CancelledAt, &item.PaidAt, &item.CancelRequestedAt, &item.CurrencyCode, &item.SubtotalMinor)
	if errors.Is(err, pgx.ErrNoRows) {
		var existingHash string
		var existingID uuid.UUID
		err = tx.QueryRow(ctx, `SELECT id, request_hash FROM orders WHERE organization_id=$1 AND idempotency_key=$2`, organizationID, key).Scan(&existingID, &existingHash)
		if err != nil {
			return Order{}, fmt.Errorf("load idempotent order: %w", err)
		}
		if existingHash != requestHash {
			return Order{}, ErrIdempotencyKeyReused
		}
		item, err = getOrderTx(ctx, tx, organizationID, existingID)
		if err != nil {
			return Order{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Order{}, fmt.Errorf("commit idempotent order: %w", err)
		}
		return item, nil
	}
	if isUniqueError(err) {
		return Order{}, ErrIdempotencyKeyReused
	}
	if err != nil {
		return Order{}, fmt.Errorf("insert order: %w", err)
	}

	ids := make([]uuid.UUID, 0, len(input.Items))
	for _, requested := range input.Items {
		ids = append(ids, requested.InventoryLevelID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	productSeen := make(map[uuid.UUID]bool, len(ids))
	snapshots := make(map[uuid.UUID]pricingSnapshot, len(ids))
	productSet := make(map[uuid.UUID]bool, len(ids))
	for _, inventoryID := range ids {
		var productID uuid.UUID
		var sku, name string
		var activeProduct, activeWarehouse bool
		err = tx.QueryRow(ctx, `SELECT i.product_id,p.sku,p.name,p.is_active,w.is_active FROM inventory_levels i
			JOIN products p ON p.id=i.product_id AND p.organization_id=i.organization_id
			JOIN warehouses w ON w.id=i.warehouse_id AND w.organization_id=i.organization_id
			WHERE i.organization_id=$1 AND i.id=$2 FOR UPDATE OF i`, organizationID, inventoryID).Scan(&productID, &sku, &name, &activeProduct, &activeWarehouse)
		if errors.Is(err, pgx.ErrNoRows) {
			return Order{}, ErrNotFound
		}
		if err != nil {
			return Order{}, fmt.Errorf("lock order inventory: %w", err)
		}
		if !activeProduct {
			return Order{}, inventory.ErrInactiveProduct
		}
		if !activeWarehouse {
			return Order{}, inventory.ErrInactiveWarehouse
		}
		if productSeen[productID] {
			return Order{}, ErrInvalidInput
		}
		productSeen[productID] = true
		snapshots[inventoryID] = pricingSnapshot{productID: productID, sku: sku, name: name}
		productSet[productID] = true
		if err := expireReservationsTx(ctx, tx, organizationID, inventoryID); err != nil {
			return Order{}, err
		}
		var onHand, reserved int64
		if err := tx.QueryRow(ctx, `SELECT i.on_hand_quantity, COALESCE(SUM(r.quantity) FILTER (WHERE ((r.status='active' AND r.expires_at > statement_timestamp()) OR r.status IN ('payment_held','committed'))),0)
			FROM inventory_levels i LEFT JOIN inventory_reservations r ON r.organization_id=i.organization_id AND r.inventory_level_id=i.id
			WHERE i.organization_id=$1 AND i.id=$2 GROUP BY i.on_hand_quantity`, organizationID, inventoryID).Scan(&onHand, &reserved); err != nil {
			return Order{}, fmt.Errorf("calculate order availability: %w", err)
		}
		for _, requested := range input.Items {
			if requested.InventoryLevelID == inventoryID && requested.Quantity > onHand-reserved {
				return Order{}, ErrInsufficientAvailableStock
			}
		}
	}
	productIDs := make([]uuid.UUID, 0, len(productSet))
	for productID := range productSet {
		productIDs = append(productIDs, productID)
	}
	sort.Slice(productIDs, func(i, j int) bool { return productIDs[i].String() < productIDs[j].String() })
	prices := make(map[uuid.UUID]pricingSnapshot, len(productIDs))
	for _, productID := range productIDs {
		var snapshot pricingSnapshot
		var active bool
		if err := tx.QueryRow(ctx, `SELECT id,sku,name,is_active,unit_price_minor,currency_code FROM products WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, productID).Scan(&snapshot.productID, &snapshot.sku, &snapshot.name, &active, &snapshot.price, &snapshot.currency); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Order{}, ErrNotFound
			}
			return Order{}, fmt.Errorf("lock product pricing: %w", err)
		}
		if !active {
			return Order{}, inventory.ErrInactiveProduct
		}
		if snapshot.price == nil || snapshot.currency == nil {
			return Order{}, ErrProductPriceMissing
		}
		if !productCurrencyAllowed(snapshot.currency) {
			return Order{}, ErrInvalidInput
		}
		prices[productID] = snapshot
	}
	var orderCurrency string
	var subtotal int64
	lineTotals := make(map[uuid.UUID]int64, len(input.Items))
	for _, requested := range input.Items {
		snapshot := prices[snapshots[requested.InventoryLevelID].productID]
		if orderCurrency == "" {
			orderCurrency = *snapshot.currency
		} else if orderCurrency != *snapshot.currency {
			return Order{}, ErrMixedCurrencyOrder
		}
		if *snapshot.price != 0 && requested.Quantity > math.MaxInt64 / *snapshot.price {
			return Order{}, ErrOrderTotalOverflow
		}
		lineTotal := *snapshot.price * requested.Quantity
		if subtotal > math.MaxInt64-lineTotal {
			return Order{}, ErrOrderTotalOverflow
		}
		subtotal += lineTotal
		lineTotals[requested.InventoryLevelID] = lineTotal
		snapshots[requested.InventoryLevelID] = snapshot
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET currency_code=$3, subtotal_minor=$4, updated_at=NOW() WHERE organization_id=$1 AND id=$2`, organizationID, item.ID, orderCurrency, subtotal); err != nil {
		return Order{}, fmt.Errorf("set order totals: %w", err)
	}
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, `SELECT NOW() + ($1 * INTERVAL '1 second')`, int64(inventory.ReservationTTL/time.Second)).Scan(&expiresAt); err != nil {
		return Order{}, fmt.Errorf("calculate order reservation expiry: %w", err)
	}
	for _, requested := range input.Items {
		var itemID uuid.UUID
		snapshot := snapshots[requested.InventoryLevelID]
		lineTotal := lineTotals[requested.InventoryLevelID]
		if err := tx.QueryRow(ctx, `INSERT INTO order_items (organization_id,order_id,product_id,sku_snapshot,product_name_snapshot,quantity,unit_price_minor_snapshot,currency_code_snapshot,line_total_minor) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, organizationID, item.ID, snapshot.productID, snapshot.sku, snapshot.name, requested.Quantity, *snapshot.price, *snapshot.currency, lineTotal).Scan(&itemID); err != nil {
			return Order{}, fmt.Errorf("insert order item: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO inventory_reservations (organization_id,inventory_level_id,order_item_id,quantity,expires_at) VALUES ($1,$2,$3,$4,$5)`, organizationID, requested.InventoryLevelID, itemID, requested.Quantity, expiresAt); err != nil {
			return Order{}, fmt.Errorf("insert order reservation: %w", err)
		}
	}
	item, err = getOrderTx(ctx, tx, organizationID, item.ID)
	if err != nil {
		return Order{}, err
	}
	writer := audit.Writer{}
	if err := writer.AppendEvent(ctx, tx, audit.Event{
		OrganizationID: organizationID,
		EventType:      "order.created",
		ResourceType:   "order",
		ResourceID:     item.ID,
		ActorType:      "user",
		ActorUserID:    &userID,
		SourceType:     "order",
		SourceID:       item.ID,
		Metadata:       map[string]any{"order_id": item.ID.String()},
	}); err != nil {
		return Order{}, fmt.Errorf("append order.created audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit order: %w", err)
	}
	return item, nil
}

func (r *Repository) List(ctx context.Context, organizationID uuid.UUID, limit int, cursor pagination.Cursor, status, search string) (ListPage, error) {
	where := "WHERE organization_id=$1"
	args := []any{organizationID}
	arg := 2
	if status != "" {
		where += fmt.Sprintf(" AND status=$%d", arg)
		args = append(args, status)
		arg++
	}
	if search != "" {
		where += fmt.Sprintf(" AND (id::text ILIKE $%d OR status ILIKE $%d OR allocation_method ILIKE $%d)", arg, arg, arg)
		args = append(args, "%"+search+"%")
		arg++
	}
	if cursor.ID != uuid.Nil {
		where += fmt.Sprintf(" AND (created_at,id)<($%d,$%d)", arg, arg+1)
		args = append(args, cursor.CreatedAt, cursor.ID)
		arg += 2
	}
	args = append(args, limit+1)
	rows, err := r.pool.Query(ctx, `SELECT `+orderColumns+` FROM orders `+where+fmt.Sprintf(` ORDER BY created_at DESC,id DESC LIMIT $%d`, arg), args...)
	if err != nil {
		return ListPage{}, fmt.Errorf("list orders: %w", err)
	}
	items := make([]Order, 0)
	for rows.Next() {
		item, scanErr := scanOrder(rows)
		if scanErr != nil {
			return ListPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListPage{}, err
	}
	rows.Close()
	page := ListPage{Orders: items}
	if len(items) > limit {
		page.Orders = items[:limit]
		page.HasMore = true
		last := page.Orders[len(page.Orders)-1]
		page.NextCursor = pagination.EncodeCursor(pagination.Cursor{OrganizationID: organizationID, CreatedAt: last.CreatedAt, ID: last.ID})
	}
	if err := loadItems(ctx, r.pool, organizationID, page.Orders); err != nil {
		return ListPage{}, err
	}
	return page, nil
}
func (r *Repository) Get(ctx context.Context, organizationID, id uuid.UUID) (Order, error) {
	item, err := scanOrder(r.pool.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE organization_id=$1 AND id=$2`, organizationID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	if err := loadOrderItems(ctx, r.pool, &item); err != nil {
		return Order{}, err
	}
	return item, nil
}

func (r *Repository) Timeline(ctx context.Context, organizationID, id uuid.UUID) ([]audit.Event, error) {
	return audit.NewReader(r.pool).OrderTimeline(ctx, organizationID, id)
}

func (r *Repository) Cancel(ctx context.Context, organizationID, orderID uuid.UUID) (Order, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT DISTINCT r.inventory_level_id FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE r.organization_id=$1 AND oi.order_id=$2`, organizationID, orderID)
	if err != nil {
		return Order{}, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return Order{}, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Order{}, err
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	for _, id := range ids {
		var locked uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM inventory_levels WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, id).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return Order{}, ErrNotFound
		}
		if err != nil {
			return Order{}, fmt.Errorf("lock inventory for order cancellation: %w", err)
		}
	}
	var item Order
	err = tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, orderID).Scan(&item.ID, &item.OrganizationID, &item.CreatedByUserID, &item.Status, &item.IdempotencyKey, &item.RequestHash, &item.AllocationMethod, &item.AllocationStrategy, &item.CreatedAt, &item.UpdatedAt, &item.CancelledAt, &item.PaidAt, &item.CancelRequestedAt, &item.CurrencyCode, &item.SubtotalMinor)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	paymentRows, queryErr := tx.Query(ctx, `SELECT id, status, provider_name FROM payments WHERE organization_id=$1 AND order_id=$2 FOR UPDATE`, organizationID, orderID)
	if queryErr != nil {
		return Order{}, fmt.Errorf("lock order payments: %w", queryErr)
	}
	providerBacked := false
	for paymentRows.Next() {
		var paymentID uuid.UUID
		var paymentStatus string
		var providerName *string
		if scanErr := paymentRows.Scan(&paymentID, &paymentStatus, &providerName); scanErr != nil {
			paymentRows.Close()
			return Order{}, scanErr
		}
		if paymentStatus == payment.StatusSucceeded {
			paymentRows.Close()
			return Order{}, payment.ErrPaymentAlreadySucceeded
		}
		providerBacked = providerBacked || providerName != nil
		_ = paymentID
	}
	if rowsErr := paymentRows.Err(); rowsErr != nil {
		paymentRows.Close()
		return Order{}, rowsErr
	}
	paymentRows.Close()
	if item.Status == StatusPending {
		if providerBacked {
			if _, err = tx.Exec(ctx, `UPDATE payments SET cancel_requested_at=COALESCE(cancel_requested_at,NOW()), close_reason='customer', next_reconcile_at=NOW(), updated_at=NOW() WHERE organization_id=$1 AND order_id=$2 AND status='pending'`, organizationID, orderID); err != nil {
				return Order{}, fmt.Errorf("request provider payment cancellation: %w", err)
			}
			if err = tx.QueryRow(ctx, `UPDATE orders SET cancel_requested_at=COALESCE(cancel_requested_at,NOW()),updated_at=NOW() WHERE organization_id=$1 AND id=$2 RETURNING `+orderColumns, organizationID, orderID).Scan(&item.ID, &item.OrganizationID, &item.CreatedByUserID, &item.Status, &item.IdempotencyKey, &item.RequestHash, &item.AllocationMethod, &item.AllocationStrategy, &item.CreatedAt, &item.UpdatedAt, &item.CancelledAt, &item.PaidAt, &item.CancelRequestedAt, &item.CurrencyCode, &item.SubtotalMinor); err != nil {
				return Order{}, err
			}
			if err = tx.Commit(ctx); err != nil {
				return Order{}, err
			}
			return item, nil
		}
		reservationRows, queryErr := tx.Query(ctx, `SELECT r.id FROM inventory_reservations r JOIN order_items oi ON oi.organization_id=r.organization_id AND oi.id=r.order_item_id WHERE r.organization_id=$1 AND oi.order_id=$2 FOR UPDATE`, organizationID, orderID)
		if queryErr != nil {
			return Order{}, queryErr
		}
		for reservationRows.Next() {
			var reservationID uuid.UUID
			if scanErr := reservationRows.Scan(&reservationID); scanErr != nil {
				reservationRows.Close()
				return Order{}, scanErr
			}
		}
		if rowsErr := reservationRows.Err(); rowsErr != nil {
			reservationRows.Close()
			return Order{}, rowsErr
		}
		reservationRows.Close()
		if _, err = tx.Exec(ctx, `UPDATE payments SET status='cancelled', updated_at=NOW() WHERE organization_id=$1 AND order_id=$2 AND status='pending'`, organizationID, orderID); err != nil {
			return Order{}, fmt.Errorf("cancel pending payments: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE inventory_reservations r SET status=CASE WHEN r.expires_at <= NOW() THEN 'expired' ELSE 'released' END, released_at=CASE WHEN r.expires_at > NOW() THEN NOW() ELSE r.released_at END, expired_at=CASE WHEN r.expires_at <= NOW() THEN NOW() ELSE r.expired_at END FROM order_items oi WHERE r.organization_id=$1 AND r.order_item_id=oi.id AND oi.organization_id=$1 AND oi.order_id=$2 AND r.status='active'`, organizationID, orderID)
		if err != nil {
			return Order{}, err
		}
		if err = tx.QueryRow(ctx, `UPDATE orders SET status='cancelled',cancelled_at=NOW() WHERE organization_id=$1 AND id=$2 RETURNING `+orderColumns, organizationID, orderID).Scan(&item.ID, &item.OrganizationID, &item.CreatedByUserID, &item.Status, &item.IdempotencyKey, &item.RequestHash, &item.AllocationMethod, &item.AllocationStrategy, &item.CreatedAt, &item.UpdatedAt, &item.CancelledAt, &item.PaidAt, &item.CancelRequestedAt, &item.CurrencyCode, &item.SubtotalMinor); err != nil {
			return Order{}, err
		}
	}
	item, err = getOrderTx(ctx, tx, organizationID, orderID)
	if err != nil {
		return Order{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return item, nil
}

func getOrderTx(ctx context.Context, tx pgx.Tx, organizationID, id uuid.UUID) (Order, error) {
	item, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE organization_id=$1 AND id=$2`, organizationID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	if err := loadOrderItems(ctx, tx, &item); err != nil {
		return Order{}, err
	}
	return item, nil
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadItems(ctx context.Context, q queryer, organizationID uuid.UUID, orders []Order) error {
	if len(orders) == 0 {
		return nil
	}
	byID := make(map[uuid.UUID]int, len(orders))
	ids := make([]uuid.UUID, len(orders))
	for i := range orders {
		byID[orders[i].ID] = i
		ids[i] = orders[i].ID
		orders[i].Items = []OrderItem{}
	}
	rows, err := q.Query(ctx, `SELECT oi.id,oi.organization_id,oi.order_id,oi.product_id,oi.sku_snapshot,oi.product_name_snapshot,oi.quantity,oi.unit_price_minor_snapshot,oi.currency_code_snapshot,oi.line_total_minor,oi.created_at,r.id,r.status,r.inventory_level_id,r.quantity,w.code FROM order_items oi LEFT JOIN inventory_reservations r ON r.organization_id=oi.organization_id AND r.order_item_id=oi.id LEFT JOIN inventory_levels il ON il.organization_id=r.organization_id AND il.id=r.inventory_level_id LEFT JOIN warehouses w ON w.organization_id=il.organization_id AND w.id=il.warehouse_id WHERE oi.organization_id=$1 AND oi.order_id=ANY($2) ORDER BY oi.created_at,oi.id,r.id`, organizationID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item OrderItem
		var inventoryLevelID *uuid.UUID
		var reservationQuantity *int64
		var warehouseCode *string
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.OrderID, &item.ProductID, &item.SKUSnapshot, &item.ProductNameSnapshot, &item.Quantity, &item.UnitPriceMinorSnapshot, &item.CurrencyCodeSnapshot, &item.LineTotalMinor, &item.CreatedAt, &item.ReservationID, &item.ReservationStatus, &inventoryLevelID, &reservationQuantity, &warehouseCode); err != nil {
			return err
		}
		if index, ok := byID[item.OrderID]; ok {
			appendOrderItemReservation(&orders[index], item, inventoryLevelID, reservationQuantity, warehouseCode)
		}
	}
	return rows.Err()
}
func loadOrderItems(ctx context.Context, q queryer, order *Order) error {
	rows, err := q.Query(ctx, `SELECT oi.id,oi.organization_id,oi.order_id,oi.product_id,oi.sku_snapshot,oi.product_name_snapshot,oi.quantity,oi.unit_price_minor_snapshot,oi.currency_code_snapshot,oi.line_total_minor,oi.created_at,r.id,r.status,r.inventory_level_id,r.quantity,w.code FROM order_items oi LEFT JOIN inventory_reservations r ON r.organization_id=oi.organization_id AND r.order_item_id=oi.id LEFT JOIN inventory_levels il ON il.organization_id=r.organization_id AND il.id=r.inventory_level_id LEFT JOIN warehouses w ON w.organization_id=il.organization_id AND w.id=il.warehouse_id WHERE oi.organization_id=$1 AND oi.order_id=$2 ORDER BY oi.created_at,oi.id,r.id`, order.OrganizationID, order.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	order.Items = []OrderItem{}
	for rows.Next() {
		var item OrderItem
		var inventoryLevelID *uuid.UUID
		var reservationQuantity *int64
		var warehouseCode *string
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.OrderID, &item.ProductID, &item.SKUSnapshot, &item.ProductNameSnapshot, &item.Quantity, &item.UnitPriceMinorSnapshot, &item.CurrencyCodeSnapshot, &item.LineTotalMinor, &item.CreatedAt, &item.ReservationID, &item.ReservationStatus, &inventoryLevelID, &reservationQuantity, &warehouseCode); err != nil {
			return err
		}
		appendOrderItemReservation(order, item, inventoryLevelID, reservationQuantity, warehouseCode)
	}
	return rows.Err()
}

func appendOrderItemReservation(order *Order, item OrderItem, inventoryLevelID *uuid.UUID, quantity *int64, warehouseCode *string) {
	var detail AllocationDetail
	if inventoryLevelID != nil && quantity != nil && warehouseCode != nil {
		detail = AllocationDetail{WarehouseCode: *warehouseCode, InventoryLevelID: *inventoryLevelID, ReservedQuantity: *quantity}
		if !containsString(order.WarehousesUsed, detail.WarehouseCode) {
			order.WarehousesUsed = append(order.WarehousesUsed, detail.WarehouseCode)
			sort.Strings(order.WarehousesUsed)
		}
	}
	for index := range order.Items {
		if order.Items[index].ID != item.ID {
			continue
		}
		if detail.InventoryLevelID != uuid.Nil {
			order.Items[index].Allocations = append(order.Items[index].Allocations, detail)
			order.Allocations = append(order.Allocations, detail)
		}
		return
	}
	if detail.InventoryLevelID != uuid.Nil {
		item.Allocations = append(item.Allocations, detail)
		order.Allocations = append(order.Allocations, detail)
	}
	order.Items = append(order.Items, item)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func expireReservationsTx(ctx context.Context, tx pgx.Tx, organizationID, inventoryID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE inventory_reservations SET status='expired',expired_at=NOW() WHERE organization_id=$1 AND inventory_level_id=$2 AND status='active' AND expires_at<=NOW()`, organizationID, inventoryID)
	return err
}
func isUniqueError(err error) bool {
	var e *pgconn.PgError
	return errors.As(err, &e) && e.Code == "23505"
}
