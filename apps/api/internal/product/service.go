package product

import (
	"context"
	"errors"
	"strings"

	"flowcart/apps/api/internal/organization"
	"github.com/google/uuid"
)

var ErrInvalidInput = errors.New("invalid product input")
var ErrForbidden = errors.New("forbidden")

type Service struct{ repository repository }

func NewService(repository repository) *Service { return &Service{repository: repository} }

func (s *Service) Create(ctx context.Context, tenant organization.TenantContext, input CreateInput) (Product, error) {
	if !canWrite(tenant.Role) {
		return Product{}, ErrForbidden
	}
	input.SKU = normalizeSKU(input.SKU)
	input.Name = strings.TrimSpace(input.Name)
	if !validSKU(input.SKU) || !validName(input.Name) {
		return Product{}, ErrInvalidInput
	}
	return s.repository.Create(ctx, tenant.OrganizationID, input)
}
func (s *Service) List(ctx context.Context, tenant organization.TenantContext) ([]Product, error) {
	return s.repository.List(ctx, tenant.OrganizationID)
}
func (s *Service) Get(ctx context.Context, tenant organization.TenantContext, id uuid.UUID) (Product, error) {
	return s.repository.Get(ctx, tenant.OrganizationID, id)
}
func (s *Service) Update(ctx context.Context, tenant organization.TenantContext, id uuid.UUID, patch PatchInput) (Product, error) {
	if !canWrite(tenant.Role) {
		return Product{}, ErrForbidden
	}
	if patch.SKU != nil {
		normalized := normalizeSKU(*patch.SKU)
		patch.SKU = &normalized
		if !validSKU(normalized) {
			return Product{}, ErrInvalidInput
		}
	}
	if patch.Name != nil {
		normalized := strings.TrimSpace(*patch.Name)
		patch.Name = &normalized
		if !validName(normalized) {
			return Product{}, ErrInvalidInput
		}
	}
	if patch.Description != nil {
		normalized := strings.TrimSpace(*patch.Description)
		patch.Description = &normalized
	}
	if patch.SKU == nil && patch.Name == nil && patch.Description == nil && patch.IsActive == nil {
		return Product{}, ErrInvalidInput
	}
	return s.repository.Update(ctx, tenant.OrganizationID, id, patch)
}
func canWrite(role organization.Role) bool {
	return role == organization.RoleOwner || role == organization.RoleAdmin
}
