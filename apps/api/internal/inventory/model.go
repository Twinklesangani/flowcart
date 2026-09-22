package inventory

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type InventoryLevel struct {
	ID                uuid.UUID `json:"id"`
	OrganizationID    uuid.UUID `json:"organization_id"`
	ProductID         uuid.UUID `json:"product_id"`
	ProductSKU        string    `json:"product_sku"`
	ProductName       string    `json:"product_name"`
	WarehouseID       uuid.UUID `json:"warehouse_id"`
	WarehouseCode     string    `json:"warehouse_code"`
	WarehouseName     string    `json:"warehouse_name"`
	OnHandQuantity    int64     `json:"on_hand_quantity"`
	ReservedQuantity  int64     `json:"reserved_quantity"`
	AvailableQuantity int64     `json:"available_quantity"`
	ReorderPoint      *int64    `json:"reorder_point,omitempty"`
	TargetStockLevel  *int64    `json:"target_stock_level,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type ListPage struct {
	Inventory  []InventoryLevel `json:"inventory"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
}

type LowStockPage struct {
	Inventory       []LowStockItem `json:"inventory"`
	NextCursor      string         `json:"next_cursor,omitempty"`
	HasMore         bool           `json:"has_more"`
	DonorsTruncated bool           `json:"donors_truncated"`
}

type CreateInput struct {
	ProductID      uuid.UUID `json:"product_id"`
	WarehouseID    uuid.UUID `json:"warehouse_id"`
	OnHandQuantity int64     `json:"on_hand_quantity"`
}

type Reservation struct {
	ID               uuid.UUID  `json:"id"`
	OrganizationID   uuid.UUID  `json:"organization_id"`
	InventoryLevelID uuid.UUID  `json:"inventory_level_id"`
	Quantity         int64      `json:"quantity"`
	Status           string     `json:"status"`
	ExpiresAt        time.Time  `json:"expires_at"`
	CreatedAt        time.Time  `json:"created_at"`
	ReleasedAt       *time.Time `json:"released_at,omitempty"`
	ExpiredAt        *time.Time `json:"expired_at,omitempty"`
}

type ReplenishmentPolicyInput struct {
	ReorderPoint     *int64 `json:"reorder_point"`
	TargetStockLevel *int64 `json:"target_stock_level"`
}

type LowStockStatus string

const (
	StatusUnconfigured LowStockStatus = "unconfigured"
	StatusOutOfStock   LowStockStatus = "out_of_stock"
	StatusLow          LowStockStatus = "low"
	StatusHealthy      LowStockStatus = "healthy"
)

type DonorAllocation struct {
	SourceWarehouseID      uuid.UUID `json:"source_warehouse_id"`
	SourceInventoryLevelID uuid.UUID `json:"source_inventory_level_id"`
	Quantity               int64     `json:"quantity"`
}

type LowStockItem struct {
	InventoryLevel
	Status              LowStockStatus    `json:"status"`
	RecommendedQuantity int64             `json:"recommended_quantity"`
	UnfulfilledQuantity int64             `json:"unfulfilled_quantity"`
	DonorsTruncated     bool              `json:"donors_truncated,omitempty"`
	DonorAllocations    []DonorAllocation `json:"donor_allocations,omitempty"`
}

const ReservationTTL = 15 * time.Minute

var (
	ErrManagedReservation         = errors.New("reservation is managed by payment/order workflow")
	ErrNotFound                   = errors.New("inventory not found")
	ErrForbidden                  = errors.New("forbidden")
	ErrInvalidInput               = errors.New("invalid inventory input")
	ErrInventoryExists            = errors.New("inventory already exists")
	ErrProductNotFound            = errors.New("product not found")
	ErrWarehouseNotFound          = errors.New("warehouse not found")
	ErrInactiveProduct            = errors.New("product is inactive")
	ErrInactiveWarehouse          = errors.New("warehouse is inactive")
	ErrInsufficientStock          = errors.New("insufficient stock")
	ErrInsufficientAvailableStock = errors.New("insufficient available stock")
	ErrStockBelowReserved         = errors.New("stock would fall below reserved quantity")
	ErrReservationNotFound        = errors.New("reservation not found")
	ErrInvalidReplenishmentPolicy = errors.New("invalid replenishment policy")
)
