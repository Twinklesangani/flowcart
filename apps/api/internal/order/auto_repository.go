package order

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"flowcart/apps/api/internal/allocation"
	"flowcart/apps/api/internal/inventory"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type autoCandidate struct {
	allocation.InventoryCandidate
	sku      string
	name     string
	price    *int64
	currency *string
}

func (r *Repository) AutoCreate(ctx context.Context, organizationID, userID uuid.UUID, key, requestHash string, input AutoCreateInput) (Order, error) {
	var existingID uuid.UUID
	var existingHash string
	err := r.pool.QueryRow(ctx, `SELECT id, request_hash FROM orders WHERE organization_id=$1 AND idempotency_key=$2`, organizationID, key).Scan(&existingID, &existingHash)
	if err == nil {
		if existingHash != requestHash {
			return Order{}, ErrIdempotencyKeyReused
		}
		return r.Get(ctx, organizationID, existingID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Order{}, fmt.Errorf("lookup automatic idempotency key: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("begin automatic order: %w", err)
	}
	defer tx.Rollback(ctx)

	var item Order
	err = tx.QueryRow(ctx, `INSERT INTO orders (organization_id,created_by_user_id,idempotency_key,request_hash,allocation_method,allocation_strategy)
        VALUES ($1,$2,$3,$4,'automatic',$5) ON CONFLICT (organization_id,idempotency_key) DO NOTHING RETURNING `+orderColumns,
		organizationID, userID, key, requestHash, allocation.StrategyMinimizeSplitsV1).Scan(
		&item.ID, &item.OrganizationID, &item.CreatedByUserID, &item.Status, &item.IdempotencyKey, &item.RequestHash,
		&item.AllocationMethod, &item.AllocationStrategy, &item.CreatedAt, &item.UpdatedAt, &item.CancelledAt,
		&item.PaidAt, &item.CancelRequestedAt, &item.CurrencyCode, &item.SubtotalMinor)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, `SELECT id, request_hash FROM orders WHERE organization_id=$1 AND idempotency_key=$2`, organizationID, key).Scan(&existingID, &existingHash); err != nil {
			return Order{}, fmt.Errorf("reload automatic idempotency key: %w", err)
		}
		if existingHash != requestHash {
			return Order{}, ErrIdempotencyKeyReused
		}
		item, err = getOrderTx(ctx, tx, organizationID, existingID)
		if err != nil {
			return Order{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Order{}, fmt.Errorf("commit automatic replay: %w", err)
		}
		return item, nil
	}
	if err != nil {
		return Order{}, fmt.Errorf("insert automatic order: %w", err)
	}

	productIDs := make([]uuid.UUID, 0, len(input.Items))
	for _, requested := range input.Items {
		productIDs = append(productIDs, requested.ProductID)
	}
	ids, err := discoverInventoryIDs(ctx, tx, organizationID, productIDs)
	if err != nil {
		return Order{}, err
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	candidates := make([]allocation.InventoryCandidate, 0, len(ids))
	prices := make(map[uuid.UUID]autoCandidate, len(productIDs))
	for _, inventoryID := range ids {
		candidate, ok, err := lockAutoCandidate(ctx, tx, organizationID, inventoryID)
		if err != nil {
			return Order{}, err
		}
		if !ok {
			continue
		}
		if err := expireReservationsTx(ctx, tx, organizationID, inventoryID); err != nil {
			return Order{}, err
		}
		available, err := effectiveAvailableTx(ctx, tx, organizationID, inventoryID, candidate.InventoryCandidate.Available)
		if err != nil {
			return Order{}, err
		}
		candidate.InventoryCandidate.Available = available
		candidates = append(candidates, candidate.InventoryCandidate)
		if _, exists := prices[candidate.ProductID]; !exists {
			prices[candidate.ProductID] = candidate
		}
	}

	// Lock every requested product after inventory locks so pricing is a stable snapshot.
	for _, productID := range productIDs {
		var candidate autoCandidate
		err := tx.QueryRow(ctx, `SELECT id,sku,name,is_active,unit_price_minor,currency_code FROM products WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, productID).Scan(&candidate.ProductID, &candidate.sku, &candidate.name, new(bool), &candidate.price, &candidate.currency)
		if errors.Is(err, pgx.ErrNoRows) {
			return Order{}, ErrNotFound
		}
		if err != nil {
			return Order{}, fmt.Errorf("lock automatic product pricing: %w", err)
		}
		var active bool
		if err := tx.QueryRow(ctx, `SELECT is_active FROM products WHERE organization_id=$1 AND id=$2`, organizationID, productID).Scan(&active); err != nil {
			return Order{}, fmt.Errorf("read automatic product state: %w", err)
		}
		if !active {
			return Order{}, inventory.ErrInactiveProduct
		}
		if candidate.price == nil || candidate.currency == nil {
			return Order{}, ErrProductPriceMissing
		}
		if !productCurrencyAllowed(candidate.currency) {
			return Order{}, ErrInvalidInput
		}
		priced := prices[productID]
		priced.sku, priced.name, priced.price, priced.currency = candidate.sku, candidate.name, candidate.price, candidate.currency
		prices[productID] = priced
	}

	result, err := allocation.Allocate(AllocationRequestFromAuto(input), candidates)
	if errors.Is(err, allocation.ErrInsufficientInventory) {
		return Order{}, ErrInsufficientNetworkStock
	}
	if err != nil {
		return Order{}, ErrInvalidInput
	}
	if len(result.Decisions) > autoReservationBudget {
		return Order{}, ErrAutomaticAllocationComplexity
	}
	byProduct := make(map[uuid.UUID][]allocation.AllocationDecision)
	for _, decision := range result.Decisions {
		byProduct[decision.ProductID] = append(byProduct[decision.ProductID], decision)
	}

	var currency string
	var subtotal int64
	lineTotals := make(map[uuid.UUID]int64, len(input.Items))
	for _, requested := range input.Items {
		price := prices[requested.ProductID].price
		if currency == "" {
			currency = *prices[requested.ProductID].currency
		} else if currency != *prices[requested.ProductID].currency {
			return Order{}, ErrMixedCurrencyOrder
		}
		if *price != 0 && requested.Quantity > math.MaxInt64 / *price {
			return Order{}, ErrOrderTotalOverflow
		}
		lineTotal := *price * requested.Quantity
		if subtotal > math.MaxInt64-lineTotal {
			return Order{}, ErrOrderTotalOverflow
		}
		subtotal += lineTotal
		lineTotals[requested.ProductID] = lineTotal
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET currency_code=$3,subtotal_minor=$4,updated_at=NOW() WHERE organization_id=$1 AND id=$2`, organizationID, item.ID, currency, subtotal); err != nil {
		return Order{}, fmt.Errorf("set automatic order totals: %w", err)
	}
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, `SELECT NOW() + ($1 * INTERVAL '1 second')`, int64(inventory.ReservationTTL/time.Second)).Scan(&expiresAt); err != nil {
		return Order{}, fmt.Errorf("calculate automatic reservation expiry: %w", err)
	}
	for _, requested := range input.Items {
		price := prices[requested.ProductID]
		var itemID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO order_items (organization_id,order_id,product_id,sku_snapshot,product_name_snapshot,quantity,unit_price_minor_snapshot,currency_code_snapshot,line_total_minor) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, organizationID, item.ID, requested.ProductID, price.sku, price.name, requested.Quantity, *price.price, *price.currency, lineTotals[requested.ProductID]).Scan(&itemID); err != nil {
			return Order{}, fmt.Errorf("insert automatic order item: %w", err)
		}
		for _, decision := range byProduct[requested.ProductID] {
			if _, err := tx.Exec(ctx, `INSERT INTO inventory_reservations (organization_id,inventory_level_id,order_item_id,quantity,expires_at) VALUES ($1,$2,$3,$4,$5)`, organizationID, decision.InventoryLevelID, itemID, decision.Quantity, expiresAt); err != nil {
				return Order{}, fmt.Errorf("insert automatic reservation: %w", err)
			}
		}
	}
	item, err = getOrderTx(ctx, tx, organizationID, item.ID)
	if err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit automatic order: %w", err)
	}
	return item, nil
}

func AllocationRequestFromAuto(input AutoCreateInput) allocation.AllocationRequest {
	items := make([]allocation.AllocationItem, len(input.Items))
	for i, item := range input.Items {
		items[i] = allocation.AllocationItem{ProductID: item.ProductID, Quantity: item.Quantity}
	}
	return allocation.AllocationRequest{Items: items}
}

const autoCandidateBudget = 64
const autoReservationBudget = 32

func discoverInventoryIDs(ctx context.Context, tx pgx.Tx, organizationID uuid.UUID, productIDs []uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM inventory_levels WHERE organization_id=$1 AND product_id=ANY($2) ORDER BY id LIMIT $3`, organizationID, productIDs, autoCandidateBudget+1)
	if err != nil {
		return nil, fmt.Errorf("discover automatic inventory: %w", err)
	}
	defer rows.Close()
	ids := make([]uuid.UUID, 0, autoCandidateBudget+1)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > autoCandidateBudget {
		return nil, ErrAutomaticAllocationComplexity
	}
	return ids, nil
}

func lockAutoCandidate(ctx context.Context, tx pgx.Tx, organizationID, inventoryID uuid.UUID) (autoCandidate, bool, error) {
	var candidate autoCandidate
	var activeProduct, activeWarehouse bool
	err := tx.QueryRow(ctx, `SELECT i.product_id,i.warehouse_id,w.code,i.on_hand_quantity,p.sku,p.name,p.is_active,w.is_active,p.unit_price_minor,p.currency_code FROM inventory_levels i JOIN products p ON p.organization_id=i.organization_id AND p.id=i.product_id JOIN warehouses w ON w.organization_id=i.organization_id AND w.id=i.warehouse_id WHERE i.organization_id=$1 AND i.id=$2 FOR UPDATE OF i, w`, organizationID, inventoryID).Scan(&candidate.ProductID, &candidate.WarehouseID, &candidate.WarehouseCode, &candidate.Available, &candidate.sku, &candidate.name, &activeProduct, &activeWarehouse, &candidate.price, &candidate.currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return autoCandidate{}, false, nil
	}
	if err != nil {
		return autoCandidate{}, false, fmt.Errorf("lock automatic inventory: %w", err)
	}
	candidate.InventoryLevelID = inventoryID
	if !activeProduct {
		return autoCandidate{}, false, inventory.ErrInactiveProduct
	}
	if !activeWarehouse {
		return autoCandidate{}, false, nil
	}
	return candidate, true, nil
}

func effectiveAvailableTx(ctx context.Context, tx pgx.Tx, organizationID, inventoryID uuid.UUID, onHand int64) (int64, error) {
	var unavailable int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(quantity) FILTER (WHERE ((status='active' AND expires_at > statement_timestamp()) OR status IN ('payment_held','committed'))),0) FROM inventory_reservations WHERE organization_id=$1 AND inventory_level_id=$2`, organizationID, inventoryID).Scan(&unavailable); err != nil {
		return 0, fmt.Errorf("calculate automatic availability: %w", err)
	}
	return onHand - unavailable, nil
}
