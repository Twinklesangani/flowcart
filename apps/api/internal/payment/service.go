package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"flowcart/apps/api/internal/organization"
	"github.com/google/uuid"
)

type repository interface {
	Create(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string) (Payment, error)
	List(context.Context, uuid.UUID, uuid.UUID) ([]Payment, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Payment, error)
}

type Service struct{ repository repository }

func NewService(repository repository) *Service { return &Service{repository: repository} }

func (s *Service) Create(ctx context.Context, tenant organization.TenantContext, orderID uuid.UUID, key string) (Payment, error) {
	if !canWrite(tenant.Role) {
		return Payment{}, ErrForbidden
	}
	key = strings.TrimSpace(key)
	if orderID == uuid.Nil || key == "" || len(key) > 200 {
		return Payment{}, ErrInvalidInput
	}
	digest := sha256.Sum256([]byte(orderID.String()))
	return s.repository.Create(ctx, tenant.OrganizationID, tenant.UserID, orderID, key, hex.EncodeToString(digest[:]))
}
func (s *Service) List(ctx context.Context, tenant organization.TenantContext, orderID uuid.UUID) ([]Payment, error) {
	return s.repository.List(ctx, tenant.OrganizationID, orderID)
}
func (s *Service) Get(ctx context.Context, tenant organization.TenantContext, paymentID uuid.UUID) (Payment, error) {
	return s.repository.Get(ctx, tenant.OrganizationID, paymentID)
}
func canWrite(role organization.Role) bool {
	return role == organization.RoleOwner || role == organization.RoleAdmin || role == organization.RoleWarehouseManager
}
