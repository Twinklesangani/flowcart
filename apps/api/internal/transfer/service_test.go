package transfer

import (
	"context"
	"errors"
	"testing"

	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/pagination"
	"github.com/google/uuid"
)

func TestTransferWriteRBAC(t *testing.T) {
	service := NewService(nil)
	input := CreateInput{SourceWarehouseID: uuid.New(), DestinationWarehouseID: uuid.New(), Items: []TransferItemInput{{ProductID: uuid.New(), Quantity: 1}}}
	for _, role := range []organization.Role{organization.RoleSupport, organization.RoleViewer} {
		tenant := organization.TenantContext{OrganizationID: uuid.New(), UserID: uuid.New(), Role: role}
		if _, err := service.Create(context.Background(), tenant, "key", input); err != ErrForbidden {
			t.Fatalf("role %s create error=%v", role, err)
		}
		if _, err := service.Dispatch(context.Background(), tenant, uuid.New(), "key"); err != ErrForbidden {
			t.Fatalf("role %s dispatch error=%v", role, err)
		}
		if _, err := service.Receive(context.Background(), tenant, uuid.New(), "key"); err != ErrForbidden {
			t.Fatalf("role %s receive error=%v", role, err)
		}
		if _, err := service.Cancel(context.Background(), tenant, uuid.New()); err != ErrForbidden {
			t.Fatalf("role %s cancel error=%v", role, err)
		}
	}
}

func TestTransferBatchLimitRejectsBeforeRepositoryMutation(t *testing.T) {
	items := make([]TransferItemInput, pagination.MaxLimit+1)
	for index := range items {
		items[index] = TransferItemInput{ProductID: uuid.New(), Quantity: 1}
	}
	service := NewService(nil)
	_, err := service.Create(context.Background(), organization.TenantContext{OrganizationID: uuid.New(), UserID: uuid.New(), Role: organization.RoleOwner}, "oversized", CreateInput{SourceWarehouseID: uuid.New(), DestinationWarehouseID: uuid.New(), Items: items})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized transfer error=%v", err)
	}
}
