package inventory

import (
	"context"

	"flowcart/apps/api/internal/organization"
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
	return s.repository.Adjust(ctx, tenant.OrganizationID, id, delta)
}
func canWrite(role organization.Role) bool {
	return role == organization.RoleOwner || role == organization.RoleAdmin || role == organization.RoleWarehouseManager
}
