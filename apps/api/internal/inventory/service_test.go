package inventory

import (
	"context"
	"errors"
	"testing"

	"flowcart/apps/api/internal/organization"
	"github.com/google/uuid"
)

type fakeRepository struct {
	createErr  error
	adjustErr  error
	reserveErr error
	releaseErr error
	lastOrgID  uuid.UUID
}

func (r *fakeRepository) Create(_ context.Context, organizationID uuid.UUID, input CreateInput) (InventoryLevel, error) {
	r.lastOrgID = organizationID
	if r.createErr != nil {
		return InventoryLevel{}, r.createErr
	}
	return InventoryLevel{ID: uuid.New(), OrganizationID: organizationID, ProductID: input.ProductID, WarehouseID: input.WarehouseID, OnHandQuantity: input.OnHandQuantity}, nil
}
func (r *fakeRepository) List(_ context.Context, organizationID uuid.UUID) ([]InventoryLevel, error) {
	r.lastOrgID = organizationID
	return nil, nil
}
func (r *fakeRepository) Get(_ context.Context, organizationID, id uuid.UUID) (InventoryLevel, error) {
	r.lastOrgID = organizationID
	return InventoryLevel{ID: id}, nil
}
func (r *fakeRepository) Adjust(_ context.Context, organizationID, _ uuid.UUID, _ int64) (InventoryLevel, error) {
	r.lastOrgID = organizationID
	return InventoryLevel{}, r.adjustErr
}
func (r *fakeRepository) Reserve(_ context.Context, organizationID, inventoryID uuid.UUID, quantity int64) (Reservation, error) {
	r.lastOrgID = organizationID
	if r.reserveErr != nil {
		return Reservation{}, r.reserveErr
	}
	return Reservation{ID: uuid.New(), OrganizationID: organizationID, InventoryLevelID: inventoryID, Quantity: quantity, Status: "active"}, nil
}
func (r *fakeRepository) Release(_ context.Context, organizationID, reservationID uuid.UUID) (Reservation, error) {
	r.lastOrgID = organizationID
	if r.releaseErr != nil {
		return Reservation{}, r.releaseErr
	}
	return Reservation{ID: reservationID, OrganizationID: organizationID, Status: "released"}, nil
}

func inventoryTenant(role organization.Role) organization.TenantContext {
	return organization.TenantContext{OrganizationID: uuid.New(), UserID: uuid.New(), Role: role}
}

func validCreateInput() CreateInput {
	return CreateInput{ProductID: uuid.New(), WarehouseID: uuid.New()}
}

func TestInventoryCreateRBAC(t *testing.T) {
	for _, role := range []organization.Role{organization.RoleOwner, organization.RoleAdmin, organization.RoleWarehouseManager} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), inventoryTenant(role), validCreateInput()); err != nil {
			t.Fatalf("%s create error = %v", role, err)
		}
	}
	for _, role := range []organization.Role{organization.RoleSupport, organization.RoleViewer} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), inventoryTenant(role), validCreateInput()); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s create error = %v", role, err)
		}
	}
}

func TestInventoryReadAllRolesAndTenantScope(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository)
	for _, role := range []organization.Role{organization.RoleOwner, organization.RoleAdmin, organization.RoleWarehouseManager, organization.RoleSupport, organization.RoleViewer} {
		tenant := inventoryTenant(role)
		if _, err := service.List(context.Background(), tenant); err != nil {
			t.Fatalf("%s list error = %v", role, err)
		}
		if repository.lastOrgID != tenant.OrganizationID {
			t.Fatalf("organization = %v, want %v", repository.lastOrgID, tenant.OrganizationID)
		}
	}
}

func TestInventoryValidationAndRepositoryErrors(t *testing.T) {
	repositoryError := errors.New("database unavailable")
	service := NewService(&fakeRepository{createErr: repositoryError})
	tenant := inventoryTenant(organization.RoleOwner)
	if _, err := service.Create(context.Background(), tenant, CreateInput{ProductID: uuid.Nil, WarehouseID: uuid.New()}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid UUID error = %v", err)
	}
	if _, err := service.Create(context.Background(), tenant, CreateInput{ProductID: uuid.New(), WarehouseID: uuid.New(), OnHandQuantity: -1}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative quantity error = %v", err)
	}
	if _, err := service.Create(context.Background(), tenant, validCreateInput()); !errors.Is(err, repositoryError) {
		t.Fatalf("repository error = %v", err)
	}
}

func TestInventoryAdjustPermissionsAndErrors(t *testing.T) {
	repository := &fakeRepository{adjustErr: ErrInsufficientStock}
	service := NewService(repository)
	if _, err := service.Adjust(context.Background(), inventoryTenant(organization.RoleSupport), uuid.New(), -1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("support adjust error = %v", err)
	}
	if _, err := service.Adjust(context.Background(), inventoryTenant(organization.RoleOwner), uuid.New(), 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero delta error = %v", err)
	}
	if _, err := service.Adjust(context.Background(), inventoryTenant(organization.RoleOwner), uuid.New(), -1); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("adjust error = %v", err)
	}
}

func TestInventoryReservationRBACAndValidation(t *testing.T) {
	service := NewService(&fakeRepository{})
	for _, role := range []organization.Role{organization.RoleOwner, organization.RoleAdmin, organization.RoleWarehouseManager} {
		if _, err := service.Reserve(context.Background(), inventoryTenant(role), uuid.New(), 1); err != nil {
			t.Fatalf("%s reserve error = %v", role, err)
		}
	}
	for _, role := range []organization.Role{organization.RoleSupport, organization.RoleViewer} {
		if _, err := service.Reserve(context.Background(), inventoryTenant(role), uuid.New(), 1); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s reserve error = %v", role, err)
		}
	}
	owner := inventoryTenant(organization.RoleOwner)
	if _, err := service.Reserve(context.Background(), owner, uuid.New(), 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero reserve error = %v", err)
	}
	if _, err := service.Reserve(context.Background(), owner, uuid.New(), -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative reserve error = %v", err)
	}
}

func TestInventoryReleaseRBACAndValidation(t *testing.T) {
	service := NewService(&fakeRepository{})
	for _, role := range []organization.Role{organization.RoleOwner, organization.RoleAdmin, organization.RoleWarehouseManager} {
		if _, err := service.Release(context.Background(), inventoryTenant(role), uuid.New()); err != nil {
			t.Fatalf("%s release error = %v", role, err)
		}
	}
	for _, role := range []organization.Role{organization.RoleSupport, organization.RoleViewer} {
		if _, err := service.Release(context.Background(), inventoryTenant(role), uuid.New()); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s release error = %v", role, err)
		}
	}
	if _, err := service.Release(context.Background(), inventoryTenant(organization.RoleOwner), uuid.Nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil release error = %v", err)
	}
}

func TestInventoryReservationRepositoryErrorsPreserved(t *testing.T) {
	reserveErr := errors.New("reservation database unavailable")
	releaseErr := errors.New("release database unavailable")
	service := NewService(&fakeRepository{reserveErr: reserveErr, releaseErr: releaseErr})
	tenant := inventoryTenant(organization.RoleOwner)
	if _, err := service.Reserve(context.Background(), tenant, uuid.New(), 1); !errors.Is(err, reserveErr) {
		t.Fatalf("reserve repository error = %v", err)
	}
	if _, err := service.Release(context.Background(), tenant, uuid.New()); !errors.Is(err, releaseErr) {
		t.Fatalf("release repository error = %v", err)
	}
}
