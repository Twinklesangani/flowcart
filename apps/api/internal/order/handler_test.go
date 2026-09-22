package order

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"flowcart/apps/api/internal/organization"
	"flowcart/apps/api/internal/pagination"
	"flowcart/apps/api/internal/payment"

	"github.com/google/uuid"
)

func TestPaidOrderCancellationIsConflict(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorFromDomain(w, fmt.Errorf("cancel order: %w", payment.ErrPaymentAlreadySucceeded))
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"payment_already_succeeded"`) {
		t.Fatalf("response = %d %s", w.Code, w.Body.String())
	}
}

func TestAllocationComplexityIsSanitized(t *testing.T) {
	w := httptest.NewRecorder()
	writeErrorFromDomain(w, fmt.Errorf("allocate order: %w", ErrAutomaticAllocationComplexity))
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"automatic_allocation_too_complex"`) {
		t.Fatalf("response = %d %s", w.Code, w.Body.String())
	}
}

type handlerRepository struct{ err error }

func (r handlerRepository) Create(context.Context, uuid.UUID, uuid.UUID, string, string, CreateInput) (Order, error) {
	return Order{}, r.err
}
func (r handlerRepository) List(context.Context, uuid.UUID, int, pagination.Cursor, string, string) (ListPage, error) {
	return ListPage{}, r.err
}
func (r handlerRepository) Get(context.Context, uuid.UUID, uuid.UUID) (Order, error) {
	return Order{}, r.err
}
func (r handlerRepository) Cancel(context.Context, uuid.UUID, uuid.UUID) (Order, error) {
	return Order{}, r.err
}
func (r handlerRepository) AutoCreate(context.Context, uuid.UUID, uuid.UUID, string, string, AutoCreateInput) (Order, error) {
	return Order{}, r.err
}

func TestAutoCreateHandlerSanitizesDomainErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
		want int
	}{
		{name: "complexity", err: ErrAutomaticAllocationComplexity, code: "automatic_allocation_too_complex", want: http.StatusUnprocessableEntity},
		{name: "shortage", err: ErrInsufficientNetworkStock, code: "insufficient_network_stock", want: http.StatusConflict},
		{name: "unexpected", err: errors.New("database exploded"), code: "internal_error", want: http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			productID := uuid.New()
			request := httptest.NewRequest(http.MethodPost, "/orders/auto-allocate", strings.NewReader(fmt.Sprintf(`{"items":[{"product_id":%q,"quantity":1}]}`, productID.String())))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "handler-test-key")
			tenant := organization.TenantContext{OrganizationID: uuid.New(), UserID: uuid.New(), Role: organization.RoleOwner}
			request = request.WithContext(organization.WithTenant(request.Context(), tenant))
			response := httptest.NewRecorder()
			NewHandler(NewService(handlerRepository{err: tc.err})).AutoCreate(response, request)
			if response.Code != tc.want || !strings.Contains(response.Body.String(), fmt.Sprintf(`"code":"%s"`, tc.code)) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			if tc.name == "unexpected" && strings.Contains(response.Body.String(), "database exploded") {
				t.Fatal("unexpected error leaked internal detail")
			}
		})
	}
}
