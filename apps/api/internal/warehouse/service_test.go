package warehouse

import (
	"context"
	"errors"
	"testing"

	"flowcart/apps/api/internal/organization"
	"github.com/google/uuid"
)

type fakeRepository struct {
	items      []Warehouse
	createErr  error
	getErr     error
	lastOrgID  uuid.UUID
	lastItemID uuid.UUID
	lastPatch  PatchInput
}

func (r *fakeRepository) Create(_ context.Context, organizationID uuid.UUID, input CreateInput) (Warehouse, error) {
	r.lastOrgID = organizationID
	if r.createErr != nil {
		return Warehouse{}, r.createErr
	}
	item := Warehouse{ID: uuid.New(), OrganizationID: organizationID, Code: input.Code, Name: input.Name, CountryCode: input.CountryCode, IsActive: input.IsActive == nil || *input.IsActive}
	r.items = append(r.items, item)
	return item, nil
}
func (r *fakeRepository) List(_ context.Context, organizationID uuid.UUID) ([]Warehouse, error) {
	r.lastOrgID = organizationID
	return r.items, nil
}
func (r *fakeRepository) Get(_ context.Context, organizationID, id uuid.UUID) (Warehouse, error) {
	r.lastOrgID, r.lastItemID = organizationID, id
	if r.getErr != nil {
		return Warehouse{}, r.getErr
	}
	return Warehouse{ID: id, OrganizationID: organizationID, Code: "MEL-01", Name: "Main"}, nil
}
func (r *fakeRepository) Update(_ context.Context, organizationID, id uuid.UUID, patch PatchInput) (Warehouse, error) {
	r.lastOrgID, r.lastItemID, r.lastPatch = organizationID, id, patch
	return Warehouse{ID: id, OrganizationID: organizationID, Code: "MEL-01", Name: "Main"}, nil
}
func tenant(role organization.Role) organization.TenantContext {
	return organization.TenantContext{OrganizationID: uuid.New(), UserID: uuid.New(), Role: role}
}

func TestWarehouseWriteRBAC(t *testing.T) {
	for _, role := range []organization.Role{organization.RoleOwner, organization.RoleAdmin, organization.RoleWarehouseManager} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), tenant(role), CreateInput{Code: "mel-01", Name: "Main"}); err != nil {
			t.Fatalf("%s create error = %v", role, err)
		}
	}
	for _, role := range []organization.Role{organization.RoleSupport, organization.RoleViewer} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), tenant(role), CreateInput{Code: "MEL-01", Name: "Main"}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s create error = %v", role, err)
		}
	}
}
func TestWarehouseReadAllRolesAndTenantScope(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository)
	for _, role := range []organization.Role{organization.RoleOwner, organization.RoleAdmin, organization.RoleWarehouseManager, organization.RoleSupport, organization.RoleViewer} {
		current := tenant(role)
		if _, err := service.List(context.Background(), current); err != nil {
			t.Fatalf("%s list error = %v", role, err)
		}
		if repository.lastOrgID != current.OrganizationID {
			t.Fatalf("organization = %v, want %v", repository.lastOrgID, current.OrganizationID)
		}
	}
}
func TestWarehouseValidationNormalizationAndPartialPatch(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository)
	current := tenant(organization.RoleWarehouseManager)
	country := "au"
	item, err := service.Create(context.Background(), current, CreateInput{Code: " mel-01 ", Name: " Main Warehouse ", CountryCode: &country})
	if err != nil || item.Code != "MEL-01" || item.Name != "Main Warehouse" || item.CountryCode == nil || *item.CountryCode != "AU" {
		t.Fatalf("created warehouse = %+v, err = %v", item, err)
	}
	if _, err := service.Update(context.Background(), current, item.ID, PatchInput{IsActive: func() *bool { value := false; return &value }()}); err != nil {
		t.Fatal(err)
	}
	if repository.lastPatch.Code != nil || repository.lastPatch.Name != nil || repository.lastPatch.IsActive == nil {
		t.Fatalf("partial patch = %+v", repository.lastPatch)
	}
	if _, err := service.Create(context.Background(), current, CreateInput{Code: "MEL 01", Name: "Main"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid code error = %v", err)
	}
	badCountry := "AUS"
	if _, err := service.Create(context.Background(), current, CreateInput{Code: "MEL-02", Name: "Main", CountryCode: &badCountry}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid country error = %v", err)
	}
}
func TestWarehouseCrossTenantAndRepositoryErrors(t *testing.T) {
	repository := &fakeRepository{getErr: ErrNotFound, createErr: errors.New("database unavailable")}
	service := NewService(repository)
	current := tenant(organization.RoleOwner)
	if _, err := service.Get(context.Background(), current, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not found error = %v", err)
	}
	if _, err := service.Create(context.Background(), current, CreateInput{Code: "MEL-01", Name: "Main"}); !errors.Is(err, repository.createErr) {
		t.Fatalf("internal error = %v", err)
	}
}
