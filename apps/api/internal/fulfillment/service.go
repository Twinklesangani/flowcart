package fulfillment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/pagination"
	"github.com/google/uuid"
)

type repository interface {
	Complete(context.Context, organization.TenantContext, uuid.UUID, string, string, []uuid.UUID) (Fulfillment, error)
	List(context.Context, uuid.UUID, uuid.UUID) ([]Fulfillment, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Fulfillment, error)
	ListMovements(context.Context, uuid.UUID, uuid.UUID, int) ([]InventoryMovement, error)
}

type Service struct{ repository repository }

func NewService(repository repository) *Service { return &Service{repository: repository} }

func (s *Service) Complete(ctx context.Context, tenant organization.TenantContext, orderID uuid.UUID, key string, input CreateInput) (Fulfillment, error) {
	if !canWrite(tenant.Role) {
		return Fulfillment{}, ErrForbidden
	}
	key = strings.TrimSpace(key)
	if orderID == uuid.Nil || key == "" || len(key) > 200 || len(input.ReservationIDs) == 0 || len(input.ReservationIDs) > pagination.MaxLimit {
		return Fulfillment{}, ErrInvalidInput
	}
	ids := append([]uuid.UUID(nil), input.ReservationIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	for i, id := range ids {
		if id == uuid.Nil || (i > 0 && ids[i-1] == id) {
			return Fulfillment{}, ErrInvalidInput
		}
	}
	canonical, err := json.Marshal(ids)
	if err != nil {
		return Fulfillment{}, ErrInvalidInput
	}
	digest := sha256.Sum256(canonical)
	return s.repository.Complete(ctx, tenant, orderID, key, hex.EncodeToString(digest[:]), ids)
}

func (s *Service) List(ctx context.Context, tenant organization.TenantContext, orderID uuid.UUID) ([]Fulfillment, error) {
	if orderID == uuid.Nil {
		return nil, ErrInvalidInput
	}
	return s.repository.List(ctx, tenant.OrganizationID, orderID)
}

func (s *Service) Get(ctx context.Context, tenant organization.TenantContext, id uuid.UUID) (Fulfillment, error) {
	if id == uuid.Nil {
		return Fulfillment{}, ErrInvalidInput
	}
	return s.repository.Get(ctx, tenant.OrganizationID, id)
}

func (s *Service) Movements(ctx context.Context, tenant organization.TenantContext, inventoryID uuid.UUID, limit int) ([]InventoryMovement, error) {
	if inventoryID == uuid.Nil {
		return nil, ErrInvalidInput
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidInput
	}
	return s.repository.ListMovements(ctx, tenant.OrganizationID, inventoryID, limit)
}

func canWrite(role organization.Role) bool {
	return role == organization.RoleOwner || role == organization.RoleAdmin || role == organization.RoleWarehouseManager
}
