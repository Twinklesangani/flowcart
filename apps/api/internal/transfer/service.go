package transfer

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
	Create(context.Context, uuid.UUID, uuid.UUID, string, string, CreateInput) (Transfer, error)
	List(context.Context, uuid.UUID, int) ([]Transfer, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (Transfer, error)
	Timeline(context.Context, uuid.UUID, uuid.UUID) ([]audit.Event, error)
	Dispatch(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) (Transfer, error)
	Receive(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) (Transfer, error)
	Cancel(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (Transfer, error)
}

type Service struct{ repository repository }

func NewService(repository repository) *Service { return &Service{repository: repository} }

func canWrite(role organization.Role) bool {
	return role == organization.RoleOwner || role == organization.RoleAdmin || role == organization.RoleWarehouseManager
}

func (s *Service) Create(ctx context.Context, tenant organization.TenantContext, key string, input CreateInput) (Transfer, error) {
	if !canWrite(tenant.Role) {
		return Transfer{}, ErrForbidden
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 200 || input.SourceWarehouseID == uuid.Nil || input.DestinationWarehouseID == uuid.Nil || input.SourceWarehouseID == input.DestinationWarehouseID || len(input.Items) == 0 || len(input.Items) > pagination.MaxLimit {
		return Transfer{}, ErrInvalidInput
	}
	items := append([]TransferItemInput(nil), input.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ProductID.String() < items[j].ProductID.String() })
	for index, item := range items {
		if item.ProductID == uuid.Nil || item.Quantity <= 0 || (index > 0 && items[index-1].ProductID == item.ProductID) {
			return Transfer{}, ErrInvalidInput
		}
	}
	input.Items = items
	canonical, err := json.Marshal(struct {
		Source      uuid.UUID           `json:"source"`
		Destination uuid.UUID           `json:"destination"`
		Items       []TransferItemInput `json:"items"`
	}{input.SourceWarehouseID, input.DestinationWarehouseID, items})
	if err != nil {
		return Transfer{}, ErrInvalidInput
	}
	digest := sha256.Sum256(canonical)
	return s.repository.Create(ctx, tenant.OrganizationID, tenant.UserID, key, hex.EncodeToString(digest[:]), input)
}

func (s *Service) List(ctx context.Context, tenant organization.TenantContext, limit int) ([]Transfer, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidInput
	}
	return s.repository.List(ctx, tenant.OrganizationID, limit)
}

func (s *Service) ListPage(ctx context.Context, tenant organization.TenantContext, limit int, cursor pagination.Cursor, status string) (ListPage, error) {
	if limit == 0 {
		limit = pagination.DefaultLimit
	}
	if limit < 1 || limit > pagination.MaxLimit {
		return ListPage{}, ErrInvalidInput
	}
	if repository, ok := s.repository.(interface {
		ListPage(context.Context, uuid.UUID, int, pagination.Cursor, string) (ListPage, error)
	}); ok {
		return repository.ListPage(ctx, tenant.OrganizationID, limit, cursor, status)
	}
	return ListPage{}, errors.New("paged transfer repository is unavailable")
}

func (s *Service) Get(ctx context.Context, tenant organization.TenantContext, id uuid.UUID) (Transfer, error) {
	if id == uuid.Nil {
		return Transfer{}, ErrNotFound
	}
	return s.repository.Get(ctx, tenant.OrganizationID, id)
}
func (s *Service) Timeline(ctx context.Context, tenant organization.TenantContext, id uuid.UUID) ([]audit.Event, error) {
	if id == uuid.Nil {
		return nil, ErrNotFound
	}
	return s.repository.Timeline(ctx, tenant.OrganizationID, id)
}

func (s *Service) Dispatch(ctx context.Context, tenant organization.TenantContext, id uuid.UUID, key string) (Transfer, error) {
	if !canWrite(tenant.Role) {
		return Transfer{}, ErrForbidden
	}
	key = strings.TrimSpace(key)
	if id == uuid.Nil || key == "" || len(key) > 200 {
		return Transfer{}, ErrInvalidInput
	}
	return s.repository.Dispatch(ctx, tenant.OrganizationID, tenant.UserID, id, key)
}

func (s *Service) Receive(ctx context.Context, tenant organization.TenantContext, id uuid.UUID, key string) (Transfer, error) {
	if !canWrite(tenant.Role) {
		return Transfer{}, ErrForbidden
	}
	key = strings.TrimSpace(key)
	if id == uuid.Nil || key == "" || len(key) > 200 {
		return Transfer{}, ErrInvalidInput
	}
	return s.repository.Receive(ctx, tenant.OrganizationID, tenant.UserID, id, key)
}

func (s *Service) Cancel(ctx context.Context, tenant organization.TenantContext, id uuid.UUID) (Transfer, error) {
	if !canWrite(tenant.Role) {
		return Transfer{}, ErrForbidden
	}
	if id == uuid.Nil {
		return Transfer{}, ErrNotFound
	}
	return s.repository.Cancel(ctx, tenant.OrganizationID, tenant.UserID, id)
}
