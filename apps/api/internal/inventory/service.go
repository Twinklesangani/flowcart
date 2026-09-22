package inventory

import (
	"context"
	"errors"
	"sort"

	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/pagination"

	"github.com/google/uuid"
)

type Service struct{ repository repository }

func NewService(repository repository) *Service { return &Service{repository: repository} }

func (s *Service) Create(ctx context.Context, tenant organization.TenantContext, input CreateInput) (InventoryLevel, error) {
	if !canWrite(tenant.Role) {
		return InventoryLevel{}, ErrForbidden
	}
	if input.ProductID == uuid.Nil || input.WarehouseID == uuid.Nil || input.OnHandQuantity < 0 {
		return InventoryLevel{}, ErrInvalidInput
	}
	return s.repository.Create(ctx, tenant.OrganizationID, input)
}
func (s *Service) List(ctx context.Context, tenant organization.TenantContext) ([]InventoryLevel, error) {
	return s.repository.List(ctx, tenant.OrganizationID)
}
func (s *Service) ListPage(ctx context.Context, tenant organization.TenantContext, limit int, cursor pagination.Cursor, warehouseID, productID uuid.UUID) (ListPage, error) {
	if limit < 1 || limit > pagination.MaxLimit {
		return ListPage{}, ErrInvalidInput
	}
	if repository, ok := s.repository.(interface {
		ListPage(context.Context, uuid.UUID, int, pagination.Cursor, uuid.UUID, uuid.UUID) (ListPage, error)
	}); ok {
		return repository.ListPage(ctx, tenant.OrganizationID, limit, cursor, warehouseID, productID)
	}
	return ListPage{}, errors.New("paged inventory repository is unavailable")
}
func (s *Service) Get(ctx context.Context, tenant organization.TenantContext, id uuid.UUID) (InventoryLevel, error) {
	return s.repository.Get(ctx, tenant.OrganizationID, id)
}
func (s *Service) Adjust(ctx context.Context, tenant organization.TenantContext, id uuid.UUID, delta int64) (InventoryLevel, error) {
	if !canWrite(tenant.Role) {
		return InventoryLevel{}, ErrForbidden
	}
	if delta == 0 {
		return InventoryLevel{}, ErrInvalidInput
	}
	if audited, ok := s.repository.(interface {
		AdjustWithUser(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int64) (InventoryLevel, error)
	}); ok {
		return audited.AdjustWithUser(ctx, tenant.OrganizationID, tenant.UserID, id, delta)
	}
	return s.repository.Adjust(ctx, tenant.OrganizationID, id, delta)
}
func (s *Service) Reserve(ctx context.Context, tenant organization.TenantContext, inventoryID uuid.UUID, quantity int64) (Reservation, error) {
	if !canWrite(tenant.Role) {
		return Reservation{}, ErrForbidden
	}
	if inventoryID == uuid.Nil || quantity <= 0 {
		return Reservation{}, ErrInvalidInput
	}
	return s.repository.Reserve(ctx, tenant.OrganizationID, inventoryID, quantity)
}
func (s *Service) Release(ctx context.Context, tenant organization.TenantContext, reservationID uuid.UUID) (Reservation, error) {
	if !canWrite(tenant.Role) {
		return Reservation{}, ErrForbidden
	}
	if reservationID == uuid.Nil {
		return Reservation{}, ErrInvalidInput
	}
	return s.repository.Release(ctx, tenant.OrganizationID, reservationID)
}

func (s *Service) UpdateReplenishmentPolicy(ctx context.Context, tenant organization.TenantContext, id uuid.UUID, input ReplenishmentPolicyInput) (InventoryLevel, error) {
	if !canWrite(tenant.Role) {
		return InventoryLevel{}, ErrForbidden
	}
	if id == uuid.Nil || !validReplenishmentPolicy(input) {
		return InventoryLevel{}, ErrInvalidReplenishmentPolicy
	}
	return s.repository.(*Repository).UpdateReplenishmentPolicy(ctx, tenant.OrganizationID, id, input)
}

func validReplenishmentPolicy(input ReplenishmentPolicyInput) bool {
	if input.ReorderPoint == nil && input.TargetStockLevel == nil {
		return true
	}
	if input.ReorderPoint == nil || input.TargetStockLevel == nil {
		return false
	}
	return *input.ReorderPoint >= 0 && *input.TargetStockLevel >= *input.ReorderPoint
}

func (s *Service) LowStock(ctx context.Context, tenant organization.TenantContext, limit int) ([]LowStockItem, error) {
	page, err := s.LowStockPage(ctx, tenant, limit, "")
	return page.Inventory, err
}

func (s *Service) LowStockPage(ctx context.Context, tenant organization.TenantContext, limit int, cursor string) (LowStockPage, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return LowStockPage{}, ErrInvalidInput
	}
	decoded, err := decodeLowStockCursor(cursor, tenant.OrganizationID)
	if err != nil {
		return LowStockPage{}, ErrInvalidInput
	}
	items, donors, next, donorsTruncated, err := s.repository.(*Repository).LowStockPage(ctx, tenant.OrganizationID, limit, decoded)
	if err != nil {
		return LowStockPage{}, err
	}
	page := LowStockPage{Inventory: applyRecommendations(items, donors, donorsTruncated), DonorsTruncated: donorsTruncated}
	if next != nil {
		page.HasMore = true
		page.NextCursor = encodeLowStockCursor(tenant.OrganizationID, *next)
	}
	return page, nil
}

func applyRecommendations(items []LowStockItem, donors []donorCandidate, donorsTruncated bool) []LowStockItem {
	byTarget := make(map[uuid.UUID][]donorCandidate)
	for _, donor := range donors {
		byTarget[donor.TargetID] = append(byTarget[donor.TargetID], donor)
	}
	for index := range items {
		item := &items[index]
		need := *item.TargetStockLevel - item.AvailableQuantity
		if need <= 0 {
			continue
		}
		candidates := make([]donorCandidate, 0)
		for _, donor := range byTarget[item.ID] {
			if donor.WarehouseID == item.WarehouseID || donor.EffectiveAvailable <= 0 {
				continue
			}
			candidates = append(candidates, donor)
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].EffectiveAvailable != candidates[j].EffectiveAvailable {
				return candidates[i].EffectiveAvailable > candidates[j].EffectiveAvailable
			}
			if candidates[i].WarehouseCode != candidates[j].WarehouseCode {
				return candidates[i].WarehouseCode < candidates[j].WarehouseCode
			}
			return candidates[i].WarehouseID.String() < candidates[j].WarehouseID.String()
		})
		if len(candidates) > replenishmentDonorLimit {
			item.DonorsTruncated = true
			candidates = candidates[:replenishmentDonorLimit]
		} else {
			item.DonorsTruncated = false
		}
		remaining := need
		for _, donor := range candidates {
			quantity := donor.EffectiveAvailable
			if quantity > remaining {
				quantity = remaining
			}
			item.DonorAllocations = append(item.DonorAllocations, DonorAllocation{SourceWarehouseID: donor.WarehouseID, SourceInventoryLevelID: donor.InventoryLevelID, Quantity: quantity})
			item.RecommendedQuantity += quantity
			remaining -= quantity
			if remaining == 0 {
				break
			}
		}
		item.UnfulfilledQuantity = remaining
	}
	return items
}

func canWrite(role organization.Role) bool {
	return role == organization.RoleOwner || role == organization.RoleAdmin || role == organization.RoleWarehouseManager
}
