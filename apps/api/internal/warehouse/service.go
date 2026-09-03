package warehouse

import (
	"context"
	"errors"
	"strings"

	"flowcart/apps/api/internal/organization"
	"github.com/google/uuid"
)

var ErrInvalidInput = errors.New("invalid warehouse input")
var ErrForbidden = errors.New("forbidden")

type Service struct{ repository repository }

func NewService(repository repository) *Service { return &Service{repository: repository} }
func (s *Service) Create(ctx context.Context, tenant organization.TenantContext, input CreateInput) (Warehouse, error) {
	if !canWrite(tenant.Role) {
		return Warehouse{}, ErrForbidden
	}
	input.Code = normalizeCode(input.Code)
	input.Name = strings.TrimSpace(input.Name)
	input.CountryCode = normalizeCountryCode(input.CountryCode)
	if !validCode(input.Code) || !validName(input.Name) || !validCountryCode(input.CountryCode) {
		return Warehouse{}, ErrInvalidInput
	}
	return s.repository.Create(ctx, tenant.OrganizationID, input)
}
func (s *Service) List(ctx context.Context, tenant organization.TenantContext) ([]Warehouse, error) {
	return s.repository.List(ctx, tenant.OrganizationID)
}
func (s *Service) Get(ctx context.Context, tenant organization.TenantContext, id uuid.UUID) (Warehouse, error) {
	return s.repository.Get(ctx, tenant.OrganizationID, id)
}
func (s *Service) Update(ctx context.Context, tenant organization.TenantContext, id uuid.UUID, patch PatchInput) (Warehouse, error) {
	if !canWrite(tenant.Role) {
		return Warehouse{}, ErrForbidden
	}
	if patch.Code != nil {
		normalized := normalizeCode(*patch.Code)
		patch.Code = &normalized
		if !validCode(normalized) {
			return Warehouse{}, ErrInvalidInput
		}
	}
	if patch.Name != nil {
		normalized := strings.TrimSpace(*patch.Name)
		patch.Name = &normalized
		if !validName(normalized) {
			return Warehouse{}, ErrInvalidInput
		}
	}
	if patch.CountryCode != nil {
		patch.CountryCode = normalizeCountryCode(patch.CountryCode)
		if !validCountryCode(patch.CountryCode) {
			return Warehouse{}, ErrInvalidInput
		}
	}
	if patch.Code == nil && patch.Name == nil && patch.AddressLine1 == nil && patch.AddressLine2 == nil && patch.City == nil && patch.State == nil && patch.PostalCode == nil && patch.CountryCode == nil && patch.IsActive == nil {
		return Warehouse{}, ErrInvalidInput
	}
	return s.repository.Update(ctx, tenant.OrganizationID, id, patch)
}
func canWrite(role organization.Role) bool {
	return role == organization.RoleOwner || role == organization.RoleAdmin || role == organization.RoleWarehouseManager
}
