package product

import (
	"context"
	"errors"
	"strings"

	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/pagination"
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
	input.CurrencyCode = normalizeCurrency(input.CurrencyCode)
	if !validSKU(input.SKU) || !validName(input.Name) || input.UnitPriceMinor == nil || *input.UnitPriceMinor < 0 || !validCurrency(input.CurrencyCode) {
		return Product{}, ErrInvalidInput
	}
	return s.repository.Create(ctx, tenant.OrganizationID, input)
}
func (s *Service) List(ctx context.Context, tenant organization.TenantContext) ([]Product, error) {
	return s.repository.List(ctx, tenant.OrganizationID)
}
func (s *Service) ListPage(ctx context.Context, tenant organization.TenantContext, limit int, cursor pagination.Cursor) (ListPage, error) {
	if limit < 1 || limit > pagination.MaxLimit {
		return ListPage{}, ErrInvalidInput
	}
	if repository, ok := s.repository.(interface {
		ListPage(context.Context, uuid.UUID, int, pagination.Cursor) (ListPage, error)
	}); ok {
		return repository.ListPage(ctx, tenant.OrganizationID, limit, cursor)
	}
	return ListPage{}, errors.New("paged product repository is unavailable")
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
	current, err := s.repository.Get(ctx, tenant.OrganizationID, id)
	if err != nil {
		return Product{}, err
	}
	resultingPrice, resultingCurrency := current.UnitPriceMinor, current.CurrencyCode
	if patch.UnitPriceMinor != nil {
		resultingPrice = patch.UnitPriceMinor
	}
	if patch.CurrencyCode != nil {
		patch.CurrencyCode = normalizeCurrency(patch.CurrencyCode)
		resultingCurrency = patch.CurrencyCode
	}
	if patch.UnitPriceMinor != nil && *patch.UnitPriceMinor < 0 {
		return Product{}, ErrInvalidInput
	}
	if patch.CurrencyCode != nil && !validCurrency(patch.CurrencyCode) {
		return Product{}, ErrInvalidInput
	}
	if (resultingPrice == nil) != (resultingCurrency == nil) {
		return Product{}, ErrInvalidInput
	}
	if patch.Description != nil {
		normalized := strings.TrimSpace(*patch.Description)
		patch.Description = &normalized
	}
	if patch.SKU == nil && patch.Name == nil && patch.Description == nil && patch.IsActive == nil && patch.UnitPriceMinor == nil && patch.CurrencyCode == nil {
		return Product{}, ErrInvalidInput
	}
	return s.repository.Update(ctx, tenant.OrganizationID, id, patch)
}
func canWrite(role organization.Role) bool {
	return role == organization.RoleOwner || role == organization.RoleAdmin
}
