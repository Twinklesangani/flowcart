package order

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending   = "pending"
	StatusCancelled = "cancelled"
)

type Order struct {
	ID                 uuid.UUID          `json:"id"`
	OrganizationID     uuid.UUID          `json:"organization_id"`
	CreatedByUserID    uuid.UUID          `json:"created_by_user_id"`
	Status             string             `json:"status"`
	IdempotencyKey     string             `json:"idempotency_key"`
	RequestHash        string             `json:"request_hash"`
	AllocationMethod   string             `json:"allocation_method"`
	AllocationStrategy *string            `json:"allocation_strategy,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
	CancelledAt        *time.Time         `json:"cancelled_at,omitempty"`
	PaidAt             *time.Time         `json:"paid_at,omitempty"`
	CancelRequestedAt  *time.Time         `json:"cancel_requested_at,omitempty"`
	CurrencyCode       *string            `json:"currency_code"`
	SubtotalMinor      *int64             `json:"subtotal_minor"`
	Items              []OrderItem        `json:"items"`
	Allocations        []AllocationDetail `json:"allocations,omitempty"`
	WarehousesUsed     []string           `json:"warehouses_used,omitempty"`
}

type ListPage struct {
	Orders     []Order `json:"orders"`
	NextCursor string  `json:"next_cursor,omitempty"`
	HasMore    bool    `json:"has_more"`
}

type OrderItem struct {
	ID                     uuid.UUID          `json:"id"`
	OrganizationID         uuid.UUID          `json:"organization_id"`
	OrderID                uuid.UUID          `json:"order_id"`
	ProductID              uuid.UUID          `json:"product_id"`
	SKUSnapshot            string             `json:"sku_snapshot"`
	ProductNameSnapshot    string             `json:"product_name_snapshot"`
	Quantity               int64              `json:"quantity"`
	UnitPriceMinorSnapshot *int64             `json:"unit_price_minor_snapshot"`
	CurrencyCodeSnapshot   *string            `json:"currency_code_snapshot"`
	LineTotalMinor         *int64             `json:"line_total_minor"`
	ReservationID          *uuid.UUID         `json:"reservation_id,omitempty"`
	ReservationStatus      *string            `json:"reservation_status,omitempty"`
	Allocations            []AllocationDetail `json:"allocations,omitempty"`
	CreatedAt              time.Time          `json:"created_at"`
}

type AllocationDetail struct {
	WarehouseCode    string    `json:"warehouse"`
	InventoryLevelID uuid.UUID `json:"inventory_level_id"`
	ReservedQuantity int64     `json:"reserved_quantity"`
}

type ItemInput struct {
	InventoryLevelID uuid.UUID `json:"inventory_level_id"`
	Quantity         int64     `json:"quantity"`
}

type CreateInput struct {
	Items []ItemInput `json:"items"`
}

type AutoItemInput struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  int64     `json:"quantity"`
}

type AutoCreateInput struct {
	Items []AutoItemInput `json:"items"`
}

var (
	ErrInvalidInput                  = errors.New("invalid order input")
	ErrForbidden                     = errors.New("forbidden")
	ErrNotFound                      = errors.New("order not found")
	ErrInsufficientAvailableStock    = errors.New("insufficient available stock")
	ErrIdempotencyKeyReused          = errors.New("idempotency key reused")
	ErrOrderNotCancellable           = errors.New("order is not cancellable")
	ErrProductPriceMissing           = errors.New("product price missing")
	ErrMixedCurrencyOrder            = errors.New("mixed currency order")
	ErrOrderTotalOverflow            = errors.New("order total overflow")
	ErrInsufficientNetworkStock      = errors.New("insufficient network stock")
	ErrNoEligibleWarehouse           = errors.New("no eligible warehouse")
	ErrAutomaticAllocationComplexity = errors.New("automatic allocation exceeds safe complexity budget")
)
