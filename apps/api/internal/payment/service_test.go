package payment

import (
	"context"
	"errors"
	"testing"

	"flowcart/apps/api/internal/organization"
	"github.com/google/uuid"
)

type fakeRepository struct{ createErr error }

func (r *fakeRepository) Create(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string) (Payment, error) {
	if r.createErr != nil {
		return Payment{}, r.createErr
	}
	return Payment{ID: uuid.New(), Status: StatusPending}, nil
}
func (r *fakeRepository) List(context.Context, uuid.UUID, uuid.UUID) ([]Payment, error) {
	return nil, nil
}
func (r *fakeRepository) Get(context.Context, uuid.UUID, uuid.UUID) (Payment, error) {
	return Payment{}, nil
}
func tenant(role organization.Role) organization.TenantContext {
	return organization.TenantContext{OrganizationID: uuid.New(), UserID: uuid.New(), Role: role}
}
func TestPaymentCreateRBAC(t *testing.T) {
	for _, role := range []organization.Role{organization.RoleOwner, organization.RoleAdmin, organization.RoleWarehouseManager} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), tenant(role), uuid.New(), "key"); err != nil {
			t.Fatalf("%s create = %v", role, err)
		}
	}
	for _, role := range []organization.Role{organization.RoleSupport, organization.RoleViewer} {
		if _, err := NewService(&fakeRepository{}).Create(context.Background(), tenant(role), uuid.New(), "key"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s create = %v", role, err)
		}
	}
}
func TestPaymentCreateValidationAndErrors(t *testing.T) {
	service := NewService(&fakeRepository{createErr: errors.New("database unavailable")})
	if _, err := service.Create(context.Background(), tenant(organization.RoleOwner), uuid.Nil, "key"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil order = %v", err)
	}
	if _, err := service.Create(context.Background(), tenant(organization.RoleOwner), uuid.New(), ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing key = %v", err)
	}
	repositoryErr := errors.New("database unavailable")
	if _, err := NewService(&fakeRepository{createErr: repositoryErr}).Create(context.Background(), tenant(organization.RoleOwner), uuid.New(), "key"); !errors.Is(err, repositoryErr) {
		t.Fatalf("repository error = %v", err)
	}
}
