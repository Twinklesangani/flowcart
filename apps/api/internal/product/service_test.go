package product

import (
	"context"
	"errors"
	"testing"

	"flowcart/apps/api/internal/organization"
	"github.com/google/uuid"
)

type fakeRepository struct {
	items                                 []Product
	createErr, listErr, getErr, updateErr error
	lastOrgID, lastItemID                 uuid.UUID
	lastPatch                             PatchInput
	currentProduct                        *Product
}

func (r *fakeRepository) Create(_ context.Context, organizationID uuid.UUID, input CreateInput) (Product, error) {
	r.lastOrgID = organizationID
	if r.createErr != nil {
		return Product{}, r.createErr
	}
	item := Product{ID: uuid.New(), OrganizationID: organizationID, SKU: input.SKU, Name: input.Name, Description: input.Description, IsActive: input.IsActive == nil || *input.IsActive, UnitPriceMinor: input.UnitPriceMinor, CurrencyCode: input.CurrencyCode}
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
	if r.currentProduct != nil {
		return *r.currentProduct, nil
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
func pricedInput(sku, name string) CreateInput {
	price := int64(1000)
	currency := "AUD"
	return CreateInput{SKU: sku, Name: name, UnitPriceMinor: &price, CurrencyCode: &currency}
}

func TestProductWriteRBAC(t *testing.T) {
	for _, role := range []organization.Role{organization.RoleOwner, organization.RoleAdmin} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), tenant(role), pricedInput("sku-1", "Cable")); err != nil {
			t.Fatalf("%s create error = %v", role, err)
		}
	}
	for _, role := range []organization.Role{organization.RoleWarehouseManager, organization.RoleSupport, organization.RoleViewer} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), tenant(role), pricedInput("SKU-1", "Cable")); !errors.Is(err, ErrForbidden) {
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
			t.Fatalf("organization = %v, want %v", repository.lastOrgID, current.OrganizationID)
		}
	}
}
func TestProductValidationNormalizationAndPartialPatch(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository)
	current := tenant(organization.RoleOwner)
	input := pricedInput(" sku-100 ", " Cable ")
	inactive := false
	input.IsActive = &inactive
	item, err := service.Create(context.Background(), current, input)
	if err != nil || item.SKU != "SKU-100" || item.Name != "Cable" || item.IsActive {
		t.Fatalf("created product = %+v, err = %v", item, err)
	}
	if _, err := service.Update(context.Background(), current, item.ID, PatchInput{IsActive: &inactive}); err != nil {
		t.Fatal(err)
	}
	if repository.lastPatch.SKU != nil || repository.lastPatch.Name != nil || repository.lastPatch.IsActive == nil || *repository.lastPatch.IsActive {
		t.Fatalf("partial patch = %+v", repository.lastPatch)
	}
	if _, err := service.Create(context.Background(), current, pricedInput("bad sku", "Cable")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid SKU error = %v", err)
	}
}
func TestProductPricingValidation(t *testing.T) {
	service := NewService(&fakeRepository{})
	current := tenant(organization.RoleOwner)
	if _, err := service.Create(context.Background(), current, CreateInput{SKU: "SKU-1", Name: "Cable"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing pricing error = %v", err)
	}
	price := int64(0)
	currency := "usd"
	item, err := service.Create(context.Background(), current, CreateInput{SKU: "SKU-2", Name: "Free Cable", UnitPriceMinor: &price, CurrencyCode: &currency})
	if err != nil || item.CurrencyCode == nil || *item.CurrencyCode != "USD" {
		t.Fatalf("zero price result = %+v, err=%v", item, err)
	}
	badCurrency := "GBP"
	if _, err := service.Create(context.Background(), current, CreateInput{SKU: "SKU-3", Name: "Cable", UnitPriceMinor: &price, CurrencyCode: &badCurrency}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsupported currency error = %v", err)
	}
	negative := int64(-1)
	if _, err := service.Create(context.Background(), current, CreateInput{SKU: "SKU-4", Name: "Cable", UnitPriceMinor: &negative, CurrencyCode: &currency}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative price error = %v", err)
	}
}
func TestProductLegacyPatchFinalState(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository)
	current := tenant(organization.RoleOwner)
	price := int64(2500)
	if _, err := service.Update(context.Background(), current, uuid.New(), PatchInput{UnitPriceMinor: &price}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("half-priced legacy patch error = %v", err)
	}
}
func TestProductPricingPatchRBACAndFinalState(t *testing.T) {
	price := int64(2500)
	currency := "AUD"
	repository := &fakeRepository{currentProduct: &Product{ID: uuid.New(), IsActive: true, UnitPriceMinor: &price, CurrencyCode: &currency}}
	service := NewService(repository)
	if _, err := service.Update(context.Background(), tenant(organization.RoleOwner), repository.currentProduct.ID, PatchInput{UnitPriceMinor: func() *int64 { v := int64(2700); return &v }()}); err != nil {
		t.Fatalf("owner price update = %v", err)
	}
	for _, role := range []organization.Role{organization.RoleWarehouseManager, organization.RoleSupport, organization.RoleViewer} {
		if _, err := service.Update(context.Background(), tenant(role), repository.currentProduct.ID, PatchInput{UnitPriceMinor: &price}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s price update = %v", role, err)
		}
	}
}
func TestProductCrossTenantAndRepositoryErrors(t *testing.T) {
	repository := &fakeRepository{getErr: ErrNotFound, createErr: errors.New("database unavailable")}
	service := NewService(repository)
	current := tenant(organization.RoleOwner)
	if _, err := service.Get(context.Background(), current, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not found error = %v", err)
	}
	if _, err := service.Create(context.Background(), current, pricedInput("SKU-1", "Cable")); !errors.Is(err, repository.createErr) {
		t.Fatalf("internal error = %v", err)
	}
}

func TestMetadataPatchDoesNotWritePreviouslyReadCurrency(t *testing.T) {
	price, currency, name := int64(2500), "AUD", "Updated name"
	repository := &fakeRepository{currentProduct: &Product{UnitPriceMinor: &price, CurrencyCode: &currency}}
	service := NewService(repository)
	if _, err := service.Update(context.Background(), tenant(organization.RoleOwner), uuid.New(), PatchInput{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if repository.lastPatch.CurrencyCode != nil || repository.lastPatch.UnitPriceMinor != nil {
		t.Fatal("metadata patch must not overwrite a concurrent pricing update")
	}
	if _, err := service.Update(context.Background(), tenant(organization.RoleOwner), uuid.New(), PatchInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty patch error = %v", err)
	}
}
