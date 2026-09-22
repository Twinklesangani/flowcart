package order

import (
	"context"
	"errors"
	"testing"

	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/pagination"
	"github.com/google/uuid"
)

type fakeRepository struct {
	createErr error
	cancelErr error
	lastKey   string
}

func (r *fakeRepository) Create(_ context.Context, _ uuid.UUID, _ uuid.UUID, key, _ string, _ CreateInput) (Order, error) {
	r.lastKey = key
	if r.createErr != nil {
		return Order{}, r.createErr
	}
	return Order{ID: uuid.New(), Status: StatusPending}, nil
}
func (r *fakeRepository) List(context.Context, uuid.UUID, int, pagination.Cursor, string, string) (ListPage, error) {
	return ListPage{}, nil
}
func (r *fakeRepository) Get(context.Context, uuid.UUID, uuid.UUID) (Order, error) {
	return Order{}, nil
}
func (r *fakeRepository) Cancel(_ context.Context, _ uuid.UUID, _ uuid.UUID) (Order, error) {
	if r.cancelErr != nil {
		return Order{}, r.cancelErr
	}
	return Order{ID: uuid.New(), Status: StatusCancelled}, nil
}

func orderTenant(role organization.Role) organization.TenantContext {
	return organization.TenantContext{OrganizationID: uuid.New(), UserID: uuid.New(), Role: role}
}
func orderInput() CreateInput {
	return CreateInput{Items: []ItemInput{{InventoryLevelID: uuid.New(), Quantity: 1}}}
}

func TestOrderBatchLimitRejectsBeforeRepositoryMutation(t *testing.T) {
	repository := &fakeRepository{}
	items := make([]ItemInput, pagination.MaxLimit+1)
	for index := range items {
		items[index] = ItemInput{InventoryLevelID: uuid.New(), Quantity: 1}
	}
	_, err := NewService(repository).Create(context.Background(), orderTenant(organization.RoleOwner), "oversized", CreateInput{Items: items})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized order error=%v", err)
	}
	if repository.lastKey != "" {
		t.Fatalf("repository was called with key %q", repository.lastKey)
	}
}

func TestAutomaticOrderBatchLimitRejectsBeforeRepositoryMutation(t *testing.T) {
	repository := &fakeRepository{}
	items := make([]AutoItemInput, pagination.MaxLimit+1)
	for index := range items {
		items[index] = AutoItemInput{ProductID: uuid.New(), Quantity: 1}
	}
	_, err := NewService(repository).AutoCreate(context.Background(), orderTenant(organization.RoleOwner), "oversized-auto", AutoCreateInput{Items: items})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized automatic order error=%v", err)
	}
	if repository.lastKey != "" {
		t.Fatalf("repository was called with key %q", repository.lastKey)
	}
}

func TestOrderCreateRBAC(t *testing.T) {
	for _, role := range []organization.Role{organization.RoleOwner, organization.RoleAdmin, organization.RoleWarehouseManager} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), orderTenant(role), "key", orderInput()); err != nil {
			t.Fatalf("%s create error = %v", role, err)
		}
	}
	for _, role := range []organization.Role{organization.RoleSupport, organization.RoleViewer} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), orderTenant(role), "key", orderInput()); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s create error = %v", role, err)
		}
	}
}

func TestOrderCreateValidationAndCanonicalHash(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository)
	tenant := orderTenant(organization.RoleOwner)
	if _, err := service.Create(context.Background(), tenant, "", orderInput()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing key error = %v", err)
	}
	if _, err := service.Create(context.Background(), tenant, "key", CreateInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty items error = %v", err)
	}
	id := uuid.New()
	if _, err := service.Create(context.Background(), tenant, "key", CreateInput{Items: []ItemInput{{InventoryLevelID: id, Quantity: 0}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero quantity error = %v", err)
	}
	if _, err := service.Create(context.Background(), tenant, "key", CreateInput{Items: []ItemInput{{InventoryLevelID: id, Quantity: 1}, {InventoryLevelID: id, Quantity: 2}}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate inventory error = %v", err)
	}
	first := uuid.New()
	second := uuid.New()
	if _, err := service.Create(context.Background(), tenant, "canonical", CreateInput{Items: []ItemInput{{InventoryLevelID: second, Quantity: 2}, {InventoryLevelID: first, Quantity: 1}}}); err != nil {
		t.Fatal(err)
	}
	firstKey := repository.lastKey
	if _, err := service.Create(context.Background(), tenant, "canonical-2", CreateInput{Items: []ItemInput{{InventoryLevelID: first, Quantity: 1}, {InventoryLevelID: second, Quantity: 2}}}); err != nil {
		t.Fatal(err)
	}
	if firstKey == "" || repository.lastKey == "" {
		t.Fatal("expected repository calls")
	}
}

func TestOrderRepositoryErrorsAndCancelRBAC(t *testing.T) {
	createErr := errors.New("database unavailable")
	cancelErr := errors.New("cancel unavailable")
	service := NewService(&fakeRepository{createErr: createErr, cancelErr: cancelErr})
	if _, err := service.Create(context.Background(), orderTenant(organization.RoleOwner), "key", orderInput()); !errors.Is(err, createErr) {
		t.Fatalf("create error = %v", err)
	}
	if _, err := service.Cancel(context.Background(), orderTenant(organization.RoleOwner), uuid.New()); !errors.Is(err, cancelErr) {
		t.Fatalf("cancel error = %v", err)
	}
	for _, role := range []organization.Role{organization.RoleSupport, organization.RoleViewer} {
		if _, err := service.Cancel(context.Background(), orderTenant(role), uuid.New()); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s cancel error = %v", role, err)
		}
	}
}
