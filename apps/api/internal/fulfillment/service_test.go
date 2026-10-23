package fulfillment

import (
	"context"
	"errors"
	"testing"

	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/pagination"

	"github.com/google/uuid"
)

type boundedListRepository struct {
	completeCalls int
	listCalls     int
	movementCalls int
}

func (r *boundedListRepository) Complete(context.Context, organization.TenantContext, uuid.UUID, string, string, []uuid.UUID) (Fulfillment, error) {
	r.completeCalls++
	return Fulfillment{}, nil
}
func (r *boundedListRepository) List(context.Context, uuid.UUID, uuid.UUID, int) ([]Fulfillment, error) {
	r.listCalls++
	return nil, nil
}
func (r *boundedListRepository) Get(context.Context, uuid.UUID, uuid.UUID) (Fulfillment, error) {
	return Fulfillment{}, nil
}
func (r *boundedListRepository) ListMovements(context.Context, uuid.UUID, uuid.UUID, int) ([]InventoryMovement, error) {
	r.movementCalls++
	return nil, nil
}

func TestFulfillmentBatchAndListBounds(t *testing.T) {
	repository := &boundedListRepository{}
	service := NewService(repository)
	tenant := organization.TenantContext{OrganizationID: uuid.New(), UserID: uuid.New(), Role: organization.RoleOwner}
	reservations := make([]uuid.UUID, pagination.MaxLimit+1)
	for index := range reservations {
		reservations[index] = uuid.New()
	}
	if _, err := service.Complete(context.Background(), tenant, uuid.New(), "batch", CreateInput{ReservationIDs: reservations}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized fulfillment batch error=%v", err)
	}
	for _, limit := range []int{0, -1, pagination.MaxLimit + 1} {
		if _, err := service.List(context.Background(), tenant, uuid.New(), limit); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("fulfillment limit %d error=%v", limit, err)
		}
	}
	for _, limit := range []int{-1, pagination.MaxLimit + 1} {
		if _, err := service.Movements(context.Background(), tenant, uuid.New(), limit); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("movement limit %d error=%v", limit, err)
		}
	}
	if repository.completeCalls != 0 || repository.listCalls != 0 || repository.movementCalls != 0 {
		t.Fatalf("invalid requests reached repository: %+v", repository)
	}
}
