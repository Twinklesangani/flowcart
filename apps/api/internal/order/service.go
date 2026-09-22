package order

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"flowcart/apps/api/internal/audit"
	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/pagination"

	"github.com/google/uuid"
)

type repository interface {
	Create(context.Context, uuid.UUID, uuid.UUID, string, string, CreateInput) (Order, error)
	List(context.Context, uuid.UUID, int, pagination.Cursor, string, string) (ListPage, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Order, error)
	Cancel(context.Context, uuid.UUID, uuid.UUID) (Order, error)
}

type timelineRepository interface {
	Timeline(context.Context, uuid.UUID, uuid.UUID) ([]audit.Event, error)
}

type automaticRepository interface {
	AutoCreate(context.Context, uuid.UUID, uuid.UUID, string, string, AutoCreateInput) (Order, error)
}

func (s *Service) AutoCreate(ctx context.Context, tenant organization.TenantContext, key string, input AutoCreateInput) (Order, error) {
	if !canWrite(tenant.Role) {
		return Order{}, ErrForbidden
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 200 || len(input.Items) == 0 || len(input.Items) > 100 {
		return Order{}, ErrInvalidInput
	}
	items := append([]AutoItemInput(nil), input.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ProductID.String() < items[j].ProductID.String() })
	seen := make(map[uuid.UUID]bool, len(items))
	for _, item := range items {
		if item.ProductID == uuid.Nil || item.Quantity <= 0 || seen[item.ProductID] {
			return Order{}, ErrInvalidInput
		}
		seen[item.ProductID] = true
	}
	canonical, err := json.Marshal(items)
	if err != nil {
		return Order{}, ErrInvalidInput
	}
	digest := sha256.Sum256(canonical)
	automatic, ok := s.repository.(automaticRepository)
	if !ok {
		return Order{}, errors.New("automatic order repository is unavailable")
	}
	return automatic.AutoCreate(ctx, tenant.OrganizationID, tenant.UserID, key, hex.EncodeToString(digest[:]), AutoCreateInput{Items: items})
}

type Service struct{ repository repository }

func NewService(repository repository) *Service { return &Service{repository: repository} }

func (s *Service) Create(ctx context.Context, tenant organization.TenantContext, key string, input CreateInput) (Order, error) {
	if !canWrite(tenant.Role) {
		return Order{}, ErrForbidden
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 200 || len(input.Items) == 0 || len(input.Items) > pagination.MaxLimit {
		return Order{}, ErrInvalidInput
	}
	items := append([]ItemInput(nil), input.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].InventoryLevelID.String() < items[j].InventoryLevelID.String() })
	seen := make(map[uuid.UUID]bool, len(items))
	for _, item := range items {
		if item.InventoryLevelID == uuid.Nil || item.Quantity <= 0 || seen[item.InventoryLevelID] {
			return Order{}, ErrInvalidInput
		}
		seen[item.InventoryLevelID] = true
	}
	canonical, err := json.Marshal(items)
	if err != nil {
		return Order{}, ErrInvalidInput
	}
	digest := sha256.Sum256(canonical)
	return s.repository.Create(ctx, tenant.OrganizationID, tenant.UserID, key, hex.EncodeToString(digest[:]), CreateInput{Items: items})
}
func (s *Service) List(ctx context.Context, tenant organization.TenantContext, limit int, cursor pagination.Cursor, status, search string) (ListPage, error) {
	if limit < 1 || limit > pagination.MaxLimit {
		return ListPage{}, ErrInvalidInput
	}
	return s.repository.List(ctx, tenant.OrganizationID, limit, cursor, status, search)
}
func (s *Service) Get(ctx context.Context, tenant organization.TenantContext, id uuid.UUID) (Order, error) {
	return s.repository.Get(ctx, tenant.OrganizationID, id)
}
func (s *Service) Timeline(ctx context.Context, tenant organization.TenantContext, id uuid.UUID) ([]audit.Event, error) {
	repository, ok := s.repository.(timelineRepository)
	if !ok {
		return nil, errors.New("order timeline repository is unavailable")
	}
	return repository.Timeline(ctx, tenant.OrganizationID, id)
}
func (s *Service) Cancel(ctx context.Context, tenant organization.TenantContext, id uuid.UUID) (Order, error) {
	if !canWrite(tenant.Role) {
		return Order{}, ErrForbidden
	}
	if id == uuid.Nil {
		return Order{}, ErrInvalidInput
	}
	return s.repository.Cancel(ctx, tenant.OrganizationID, id)
}
func canWrite(role organization.Role) bool {
	return role == organization.RoleOwner || role == organization.RoleAdmin || role == organization.RoleWarehouseManager
}

var _ = errors.Is
