package product

import (
	"context"
	"errors"
	"testing"

	"flowcart/apps/api/internal/organization"
	"github.com/google/uuid"
)

type fakeRepository struct {
	items      []Product
	createErr  error
	listErr    error
	getErr     error
	updateErr  error
	lastOrgID  uuid.UUID
	lastItemID uuid.UUID
	lastPatch  PatchInput
}

func (r *fakeRepository) Create(_ context.Context, organizationID uuid.UUID, input CreateInput) (Product, error) {
	r.lastOrgID = organizationID
	if r.createErr != nil {
		return Product{}, r.createErr
	}
	item := Product{ID: uuid.New(), OrganizationID: organizationID, SKU: input.SKU, Name: input.Name, Description: input.Description, IsActive: input.IsActive == nil || *input.IsActive}
	r.items = append(r.items, item)
	return item, nil
}
func (r *fakeRepository) List(_ context.Context, organizationID uuid.UUID) ([]Product, error) {
	r.lastOrgID = organizationID
	return r.items, r.listErr
}
func (r *fakeRepository) Get(_ context.Context, organizationID, id uuid.UUID) (Product, error) {
	r.lastOrgID, r.lastItemID = organizationID, id
	if r.getErr != nil {
		return Product{}, r.getErr
	}
	return Product{ID: id, OrganizationID: organizationID, SKU: "SKU-1", Name: "Cable", IsActive: true}, nil
}
func (r *fakeRepository) Update(_ context.Context, organizationID, id uuid.UUID, patch PatchInput) (Product, error) {
	r.lastOrgID, r.lastItemID, r.lastPatch = organizationID, id, patch
	if r.updateErr != nil {
		return Product{}, r.updateErr
	}
	return Product{ID: id, OrganizationID: organizationID, SKU: "SKU-1", Name: "Cable", IsActive: true}, nil
}

func tenant(role organization.Role) organization.TenantContext {
	return organization.TenantContext{OrganizationID: uuid.New(), UserID: uuid.New(), Role: role}
}

func TestProductWriteRBAC(t *testing.T) {
	for _, role := range []organization.Role{organization.RoleOwner, organization.RoleAdmin} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), tenant(role), CreateInput{SKU: "sku-1", Name: "Cable"}); err != nil {
			t.Fatalf("%s create error = %v", role, err)
		}
	}
	for _, role := range []organization.Role{organization.RoleWarehouseManager, organization.RoleSupport, organization.RoleViewer} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), tenant(role), CreateInput{SKU: "SKU-1", Name: "Cable"}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s create error = %v", role, err)
		}
	}
}
func TestProductReadAllRolesAndTenantScope(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository)
	for _, role := range []organization.Role{organization.RoleOwner, organization.RoleAdmin, organization.RoleWarehouseManager, organization.RoleSupport, organization.RoleViewer} {
		current := tenant(role)
		if _, err := service.List(context.Background(), current); err != nil {
			t.Fatalf("%s list error = %v", role, err)
		}
		if repository.lastOrgID != current.OrganizationID {
			t.Fatalf("list organization = %v, want %v", repository.lastOrgID, current.OrganizationID)
		}
	}
}
func TestProductValidationNormalizationAndPartialPatch(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository)
	current := tenant(organization.RoleOwner)
	item, err := service.Create(context.Background(), current, CreateInput{SKU: " sku-100 ", Name: " Cable ", IsActive: func() *bool { value := false; return &value }()})
	if err != nil || item.SKU != "SKU-100" || item.Name != "Cable" || item.IsActive {
		t.Fatalf("created product = %+v, err = %v", item, err)
	}
	if _, err := service.Update(context.Background(), current, item.ID, PatchInput{IsActive: func() *bool { value := false; return &value }()}); err != nil {
		t.Fatal(err)
	}
	if repository.lastPatch.SKU != nil || repository.lastPatch.Name != nil || repository.lastPatch.IsActive == nil || *repository.lastPatch.IsActive {
		t.Fatalf("partial patch = %+v", repository.lastPatch)
	}
	if _, err := service.Create(context.Background(), current, CreateInput{SKU: "bad sku", Name: "Cable"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid SKU error = %v", err)
	}
}
func TestProductCrossTenantAndRepositoryErrors(t *testing.T) {
	repository := &fakeRepository{getErr: ErrNotFound, createErr: errors.New("database unavailable")}
	service := NewService(repository)
	current := tenant(organization.RoleOwner)
	if _, err := service.Get(context.Background(), current, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not found error = %v", err)
	}
	if _, err := service.Create(context.Background(), current, CreateInput{SKU: "SKU-1", Name: "Cable"}); !errors.Is(err, repository.createErr) {
		t.Fatalf("internal error = %v", err)
	}
}
