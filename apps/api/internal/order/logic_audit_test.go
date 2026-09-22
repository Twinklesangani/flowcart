package order

import (
	"github.com/google/uuid"
	"reflect"
	"testing"
)

func TestSplitItemIncludesEveryWarehouse(t *testing.T) {
	item := OrderItem{ID: uuid.New(), Quantity: 4}
	result := Order{}
	quantity := int64(2)
	for _, code := range []string{"SYD", "MEL"} {
		inventory := uuid.New()
		appendOrderItemReservation(&result, item, &inventory, &quantity, &code)
	}
	if len(result.Items) != 1 || result.Items[0].Quantity != 4 || len(result.Items[0].Allocations) != 2 || len(result.Allocations) != 2 {
		t.Fatalf("split item shape: %+v", result)
	}
	if !reflect.DeepEqual(result.WarehousesUsed, []string{"MEL", "SYD"}) {
		t.Fatalf("warehouses = %v", result.WarehousesUsed)
	}
}

func TestDistinctReservationsWithIdenticalVisibleFieldsRemainSeparate(t *testing.T) {
	item := OrderItem{ID: uuid.New(), Quantity: 4}
	result := Order{}
	warehouse := "MEL"
	quantity := int64(2)
	inventory := uuid.New()
	firstReservation := uuid.New()
	secondReservation := uuid.New()

	first := item
	first.ReservationID = &firstReservation
	second := item
	second.ReservationID = &secondReservation
	appendOrderItemReservation(&result, first, &inventory, &quantity, &warehouse)
	appendOrderItemReservation(&result, second, &inventory, &quantity, &warehouse)

	if len(result.Items) != 1 {
		t.Fatalf("order item rows = %d, want 1", len(result.Items))
	}
	if len(result.Items[0].Allocations) != 2 {
		t.Fatalf("item allocations = %+v, want 2 allocations", result.Items[0].Allocations)
	}
	if len(result.Allocations) != 2 {
		t.Fatalf("order allocations = %+v, want 2 allocations", result.Allocations)
	}
	if !reflect.DeepEqual(result.WarehousesUsed, []string{"MEL"}) {
		t.Fatalf("warehouses = %v", result.WarehousesUsed)
	}
}
