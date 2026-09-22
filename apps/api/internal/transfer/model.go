package transfer

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending   = "pending"
	StatusInTransit = "in_transit"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

type Transfer struct {
	ID                       uuid.UUID      `json:"id"`
	OrganizationID           uuid.UUID      `json:"organization_id"`
	SourceWarehouseID        uuid.UUID      `json:"source_warehouse_id"`
	DestinationWarehouseID   uuid.UUID      `json:"destination_warehouse_id"`
	SourceWarehouseCode      string         `json:"source_warehouse_code"`
	DestinationWarehouseCode string         `json:"destination_warehouse_code"`
	Status                   string         `json:"status"`
	CreatedByUserID          uuid.UUID      `json:"created_by_user_id"`
	DispatchedByUserID       *uuid.UUID     `json:"dispatched_by_user_id,omitempty"`
	ReceivedByUserID         *uuid.UUID     `json:"received_by_user_id,omitempty"`
	CancelledByUserID        *uuid.UUID     `json:"cancelled_by_user_id,omitempty"`
	CreatedAt                time.Time      `json:"created_at"`
	DispatchedAt             *time.Time     `json:"dispatched_at,omitempty"`
	CompletedAt              *time.Time     `json:"completed_at,omitempty"`
	CancelledAt              *time.Time     `json:"cancelled_at,omitempty"`
	Items                    []TransferItem `json:"items"`
	CreationRequestHash      string         `json:"-"`
	DispatchKey              string         `json:"-"`
	ReceiveKey               string         `json:"-"`
}

type ListPage struct {
	Transfers  []Transfer `json:"transfers"`
	NextCursor string     `json:"next_cursor,omitempty"`
	HasMore    bool       `json:"has_more"`
}

type TransferItem struct {
	ID                          uuid.UUID `json:"id"`
	OrganizationID              uuid.UUID `json:"organization_id"`
	TransferID                  uuid.UUID `json:"transfer_id"`
	ProductID                   uuid.UUID `json:"product_id"`
	ProductSKU                  string    `json:"product_sku"`
	ProductName                 string    `json:"product_name"`
	SourceInventoryLevelID      uuid.UUID `json:"source_inventory_level_id"`
	DestinationInventoryLevelID uuid.UUID `json:"destination_inventory_level_id"`
	Quantity                    int64     `json:"quantity"`
	CreatedAt                   time.Time `json:"created_at"`
}

type CreateInput struct {
	SourceWarehouseID      uuid.UUID           `json:"source_warehouse_id"`
	DestinationWarehouseID uuid.UUID           `json:"destination_warehouse_id"`
	Items                  []TransferItemInput `json:"items"`
}

type TransferItemInput struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  int64     `json:"quantity"`
}

var (
	ErrInvalidInput                  = errors.New("invalid transfer input")
	ErrForbidden                     = errors.New("forbidden")
	ErrNotFound                      = errors.New("transfer not found")
	ErrSameWarehouse                 = errors.New("source and destination warehouses must differ")
	ErrIdempotencyKeyReused          = errors.New("idempotency key reused")
	ErrTransferNotPending            = errors.New("transfer is not pending")
	ErrTransferNotInTransit          = errors.New("transfer is not in transit")
	ErrTransferAlreadyCompleted      = errors.New("transfer is already completed")
	ErrInsufficientTransferableStock = errors.New("insufficient transferable stock")
	ErrInactiveWarehouse             = errors.New("warehouse is inactive")
	ErrInactiveProduct               = errors.New("product is inactive")
	ErrSourceInventoryNotFound       = errors.New("source inventory not found")
)
