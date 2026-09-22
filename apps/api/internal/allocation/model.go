package allocation

import "github.com/google/uuid"

type AllocationRequest struct {
	Items []AllocationItem
}

type AllocationItem struct {
	ProductID uuid.UUID
	Quantity  int64
}

type InventoryCandidate struct {
	InventoryLevelID uuid.UUID
	WarehouseID      uuid.UUID
	WarehouseCode    string
	ProductID        uuid.UUID
	Available        int64
}

type AllocationDecision struct {
	ProductID        uuid.UUID
	InventoryLevelID uuid.UUID
	WarehouseID      uuid.UUID
	WarehouseCode    string
	Quantity         int64
}

type AllocationResult struct {
	Decisions []AllocationDecision
}
