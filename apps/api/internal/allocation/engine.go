package allocation

import (
	"errors"
	"sort"

	"github.com/google/uuid"
)

var (
	ErrInvalidRequest        = errors.New("invalid allocation request")
	ErrInsufficientInventory = errors.New("insufficient inventory")
)

const StrategyMinimizeSplitsV1 = "minimize_splits_v1"

type warehouseSnapshot struct {
	id          uuid.UUID
	code        string
	candidates  []InventoryCandidate
	availableBy map[uuid.UUID]int64
}

func Allocate(request AllocationRequest, candidates []InventoryCandidate) (AllocationResult, error) {
	remaining, err := normalizeRequest(request)
	if err != nil {
		return AllocationResult{}, err
	}

	warehouses := groupWarehouses(candidates)
	if len(warehouses) == 0 || !networkCanSatisfy(remaining, candidates) {
		return AllocationResult{}, ErrInsufficientInventory
	}

	if warehouse := bestSingleWarehouse(remaining, warehouses); warehouse != nil {
		return allocateFromWarehouses(remaining, []*warehouseSnapshot{warehouse}), nil
	}

	result := AllocationResult{}
	for hasRemaining(remaining) {
		best := bestGreedyWarehouse(remaining, warehouses)
		if best == nil {
			return AllocationResult{}, ErrInsufficientInventory
		}
		decisions := allocateFromWarehouse(remaining, best)
		result.Decisions = append(result.Decisions, decisions...)
		for _, decision := range decisions {
			remaining[decision.ProductID] -= decision.Quantity
			best.availableBy[decision.ProductID] -= decision.Quantity
		}
	}
	return result, nil
}

func normalizeRequest(request AllocationRequest) (map[uuid.UUID]int64, error) {
	if len(request.Items) == 0 {
		return nil, ErrInvalidRequest
	}
	remaining := make(map[uuid.UUID]int64, len(request.Items))
	for _, item := range request.Items {
		if item.ProductID == uuid.Nil || item.Quantity <= 0 {
			return nil, ErrInvalidRequest
		}
		if _, exists := remaining[item.ProductID]; exists {
			return nil, ErrInvalidRequest
		}
		remaining[item.ProductID] = item.Quantity
	}
	return remaining, nil
}

func groupWarehouses(candidates []InventoryCandidate) []*warehouseSnapshot {
	byID := make(map[uuid.UUID]*warehouseSnapshot)
	for _, candidate := range candidates {
		if candidate.InventoryLevelID == uuid.Nil || candidate.WarehouseID == uuid.Nil || candidate.ProductID == uuid.Nil || candidate.Available <= 0 {
			continue
		}
		warehouse := byID[candidate.WarehouseID]
		if warehouse == nil {
			warehouse = &warehouseSnapshot{id: candidate.WarehouseID, code: candidate.WarehouseCode, availableBy: make(map[uuid.UUID]int64)}
			byID[candidate.WarehouseID] = warehouse
		}
		warehouse.candidates = append(warehouse.candidates, candidate)
		warehouse.availableBy[candidate.ProductID] += candidate.Available
	}
	warehouses := make([]*warehouseSnapshot, 0, len(byID))
	for _, warehouse := range byID {
		sort.Slice(warehouse.candidates, func(i, j int) bool {
			return warehouse.candidates[i].InventoryLevelID.String() < warehouse.candidates[j].InventoryLevelID.String()
		})
		warehouses = append(warehouses, warehouse)
	}
	sort.Slice(warehouses, func(i, j int) bool { return warehouseLess(warehouses[i], warehouses[j]) })
	return warehouses
}

func networkCanSatisfy(remaining map[uuid.UUID]int64, candidates []InventoryCandidate) bool {
	total := make(map[uuid.UUID]int64)
	for _, candidate := range candidates {
		if candidate.Available > 0 {
			total[candidate.ProductID] += candidate.Available
		}
	}
	for productID, quantity := range remaining {
		if total[productID] < quantity {
			return false
		}
	}
	return true
}

func bestSingleWarehouse(remaining map[uuid.UUID]int64, warehouses []*warehouseSnapshot) *warehouseSnapshot {
	var best *warehouseSnapshot
	for _, warehouse := range warehouses {
		fits := true
		for productID, quantity := range remaining {
			if warehouse.availableBy[productID] < quantity {
				fits = false
				break
			}
		}
		if fits && (best == nil || singleWarehouseLess(warehouse, best, remaining)) {
			best = warehouse
		}
	}
	return best
}

func bestGreedyWarehouse(remaining map[uuid.UUID]int64, warehouses []*warehouseSnapshot) *warehouseSnapshot {
	var best *warehouseSnapshot
	for _, warehouse := range warehouses {
		if len(allocateFromWarehouse(remaining, warehouse)) == 0 {
			continue
		}
		if best == nil || greedyWarehouseLess(warehouse, best, remaining) {
			best = warehouse
		}
	}
	return best
}

func allocateFromWarehouses(remaining map[uuid.UUID]int64, warehouses []*warehouseSnapshot) AllocationResult {
	result := AllocationResult{}
	for _, warehouse := range warehouses {
		decisions := allocateFromWarehouse(remaining, warehouse)
		result.Decisions = append(result.Decisions, decisions...)
		for _, decision := range decisions {
			remaining[decision.ProductID] -= decision.Quantity
		}
	}
	return result
}

func allocateFromWarehouse(remaining map[uuid.UUID]int64, warehouse *warehouseSnapshot) []AllocationDecision {
	decisions := make([]AllocationDecision, 0)
	for _, candidate := range warehouse.candidates {
		quantity := remaining[candidate.ProductID]
		if quantity <= 0 {
			continue
		}
		if candidate.Available < quantity {
			quantity = candidate.Available
		}
		if quantity > 0 {
			decisions = append(decisions, AllocationDecision{ProductID: candidate.ProductID, InventoryLevelID: candidate.InventoryLevelID, WarehouseID: candidate.WarehouseID, WarehouseCode: candidate.WarehouseCode, Quantity: quantity})
		}
	}
	return decisions
}

func hasRemaining(remaining map[uuid.UUID]int64) bool {
	for _, quantity := range remaining {
		if quantity > 0 {
			return true
		}
	}
	return false
}

func singleWarehouseLess(left, right *warehouseSnapshot, remaining map[uuid.UUID]int64) bool {
	leftHeadroom, rightHeadroom := warehouseHeadroom(left, remaining), warehouseHeadroom(right, remaining)
	if leftHeadroom != rightHeadroom {
		return leftHeadroom > rightHeadroom
	}
	return warehouseLess(left, right)
}

func greedyWarehouseLess(left, right *warehouseSnapshot, remaining map[uuid.UUID]int64) bool {
	leftLines, leftUnits, leftTouched := warehouseScore(left, remaining)
	rightLines, rightUnits, rightTouched := warehouseScore(right, remaining)
	if leftLines != rightLines {
		return leftLines > rightLines
	}
	if leftUnits != rightUnits {
		return leftUnits > rightUnits
	}
	if leftTouched != rightTouched {
		return leftTouched < rightTouched
	}
	leftHeadroom, rightHeadroom := warehouseHeadroom(left, remaining), warehouseHeadroom(right, remaining)
	if leftHeadroom != rightHeadroom {
		return leftHeadroom > rightHeadroom
	}
	return warehouseLess(left, right)
}

func warehouseScore(warehouse *warehouseSnapshot, remaining map[uuid.UUID]int64) (int, int64, int) {
	lines, units, touched := 0, int64(0), 0
	for productID, quantity := range remaining {
		available := warehouse.availableBy[productID]
		if available <= 0 {
			continue
		}
		touched++
		if available >= quantity {
			lines++
		}
		if available < quantity {
			units += available
		} else {
			units += quantity
		}
	}
	return lines, units, touched
}

func warehouseHeadroom(warehouse *warehouseSnapshot, remaining map[uuid.UUID]int64) int64 {
	var headroom int64
	for productID, quantity := range remaining {
		if available := warehouse.availableBy[productID] - quantity; available > 0 {
			headroom += available
		}
	}
	return headroom
}

func warehouseLess(left, right *warehouseSnapshot) bool {
	if left.code != right.code {
		return left.code < right.code
	}
	return left.id.String() < right.id.String()
}
