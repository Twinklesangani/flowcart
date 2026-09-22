package fulfillment

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	StatusCompleted = "completed"
)

type Fulfillment struct {
	ID              uuid.UUID         `json:"id"`
	OrganizationID  uuid.UUID         `json:"organization_id"`
	OrderID         uuid.UUID         `json:"order_id"`
	WarehouseID     uuid.UUID         `json:"warehouse_id"`
	WarehouseCode   string            `json:"warehouse_code"`
	WarehouseName   string            `json:"warehouse_name"`
	CreatedByUserID uuid.UUID         `json:"created_by_user_id"`
	Status          string            `json:"status"`
	IdempotencyKey  string            `json:"idempotency_key"`
	RequestHash     string            `json:"-"`
	CreatedAt       time.Time         `json:"created_at"`
	CompletedAt     time.Time         `json:"completed_at"`
	Items           []FulfillmentItem `json:"items"`
}

type FulfillmentItem struct {
	ID               uuid.UUID `json:"id"`
	OrganizationID   uuid.UUID `json:"organization_id"`
	FulfillmentID    uuid.UUID `json:"fulfillment_id"`
	OrderID          uuid.UUID `json:"order_id"`
	OrderItemID      uuid.UUID `json:"order_item_id"`
	ReservationID    uuid.UUID `json:"reservation_id"`
	InventoryLevelID uuid.UUID `json:"inventory_level_id"`
	WarehouseID      uuid.UUID `json:"warehouse_id"`
	Quantity         int64     `json:"quantity"`
}

type InventoryMovement struct {
	ID               uuid.UUID  `json:"id"`
	OrganizationID   uuid.UUID  `json:"organization_id"`
	InventoryLevelID uuid.UUID  `json:"inventory_level_id"`
	ProductID        uuid.UUID  `json:"product_id"`
	WarehouseID      uuid.UUID  `json:"warehouse_id"`
	MovementType     string     `json:"movement_type"`
	QuantityDelta    int64      `json:"quantity_delta"`
	FulfillmentID    *uuid.UUID `json:"fulfillment_id,omitempty"`
	ReservationID    *uuid.UUID `json:"reservation_id,omitempty"`
	CreatedByUserID  *uuid.UUID `json:"created_by_user_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

type CreateInput struct {
	ReservationIDs []uuid.UUID `json:"reservation_ids"`
}

var (
	ErrInvalidInput               = errors.New("invalid fulfillment input")
	ErrForbidden                  = errors.New("forbidden")
	ErrNotFound                   = errors.New("fulfillment not found")
	ErrOrderNotFound              = errors.New("order not found")
	ErrOrderNotPaid               = errors.New("order is not paid")
	ErrOrderAlreadyFulfilled      = errors.New("order is already fulfilled")
	ErrReservationNotFound        = errors.New("reservation not found")
	ErrReservationNotCommitted    = errors.New("reservation is not committed")
	ErrReservationAlreadyConsumed = errors.New("reservation is already consumed")
	ErrReservationOtherOrder      = errors.New("reservation belongs to another order")
	ErrMixedWarehouse             = errors.New("reservations belong to different warehouses")
	ErrInsufficientStock          = errors.New("insufficient stock for fulfillment")
	ErrIdempotencyKeyReused       = errors.New("idempotency key reused")
)
