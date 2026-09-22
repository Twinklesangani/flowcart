package allocation

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestAllocatePrefersOneWarehouseThatSatisfiesEveryProduct(t *testing.T) {
	productA, productB := uuid.MustParse("00000000-0000-0000-0000-000000000001"), uuid.MustParse("00000000-0000-0000-0000-000000000002")
	warehouseOne := uuid.MustParse("00000000-0000-0000-0000-000000000011")
	result, err := Allocate(AllocationRequest{Items: []AllocationItem{{ProductID: productA, Quantity: 3}, {ProductID: productB, Quantity: 2}}}, []InventoryCandidate{
		{InventoryLevelID: uuid.MustParse("00000000-0000-0000-0000-000000000101"), WarehouseID: warehouseOne, WarehouseCode: "WH-1", ProductID: productA, Available: 5},
		{InventoryLevelID: uuid.MustParse("00000000-0000-0000-0000-000000000102"), WarehouseID: warehouseOne, WarehouseCode: "WH-1", ProductID: productB, Available: 3},
		{InventoryLevelID: uuid.MustParse("00000000-0000-0000-0000-000000000201"), WarehouseID: uuid.MustParse("00000000-0000-0000-0000-000000000022"), WarehouseCode: "WH-2", ProductID: productA, Available: 10},
		{InventoryLevelID: uuid.MustParse("00000000-0000-0000-0000-000000000301"), WarehouseID: uuid.MustParse("00000000-0000-0000-0000-000000000033"), WarehouseCode: "WH-3", ProductID: productB, Available: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Decisions) != 2 || result.Decisions[0].WarehouseID != warehouseOne || result.Decisions[1].WarehouseID != warehouseOne {
		t.Fatalf("expected both products from WH-1, got %+v", result.Decisions)
	}
}

func TestAllocateSplitsSameProductDeterministically(t *testing.T) {
	product := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	candidates := []InventoryCandidate{
		{InventoryLevelID: uuid.MustParse("00000000-0000-0000-0000-000000000101"), WarehouseID: uuid.MustParse("00000000-0000-0000-0000-000000000011"), WarehouseCode: "WH-1", ProductID: product, Available: 5},
		{InventoryLevelID: uuid.MustParse("00000000-0000-0000-0000-000000000201"), WarehouseID: uuid.MustParse("00000000-0000-0000-0000-000000000022"), WarehouseCode: "WH-2", ProductID: product, Available: 4},
	}
	result, err := Allocate(AllocationRequest{Items: []AllocationItem{{ProductID: product, Quantity: 8}}}, candidates)
	if err != nil {
		t.Fatal(err)
	}
	want := []AllocationDecision{{ProductID: product, InventoryLevelID: candidates[0].InventoryLevelID, WarehouseID: candidates[0].WarehouseID, WarehouseCode: "WH-1", Quantity: 5}, {ProductID: product, InventoryLevelID: candidates[1].InventoryLevelID, WarehouseID: candidates[1].WarehouseID, WarehouseCode: "WH-2", Quantity: 3}}
	if !reflect.DeepEqual(result.Decisions, want) {
		t.Fatalf("got %+v, want %+v", result.Decisions, want)
	}

	reversed, err := Allocate(AllocationRequest{Items: []AllocationItem{{ProductID: product, Quantity: 8}}}, []InventoryCandidate{candidates[1], candidates[0]})
	if err != nil || !reflect.DeepEqual(reversed, result) {
		t.Fatalf("reversed candidates changed result: got %+v, err %v", reversed, err)
	}
}

func TestAllocateGreedyFallbackScoresCompleteLinesBeforeUnits(t *testing.T) {
	productA, productB := uuid.MustParse("00000000-0000-0000-0000-000000000001"), uuid.MustParse("00000000-0000-0000-0000-000000000002")
	result, err := Allocate(AllocationRequest{Items: []AllocationItem{{ProductID: productA, Quantity: 5}, {ProductID: productB, Quantity: 5}}}, []InventoryCandidate{
		{InventoryLevelID: uuid.MustParse("00000000-0000-0000-0000-000000000101"), WarehouseID: uuid.MustParse("00000000-0000-0000-0000-000000000011"), WarehouseCode: "WH-1", ProductID: productA, Available: 5},
		{InventoryLevelID: uuid.MustParse("00000000-0000-0000-0000-000000000102"), WarehouseID: uuid.MustParse("00000000-0000-0000-0000-000000000011"), WarehouseCode: "WH-1", ProductID: productB, Available: 1},
		{InventoryLevelID: uuid.MustParse("00000000-0000-0000-0000-000000000201"), WarehouseID: uuid.MustParse("00000000-0000-0000-0000-000000000022"), WarehouseCode: "WH-2", ProductID: productB, Available: 5},
		{InventoryLevelID: uuid.MustParse("00000000-0000-0000-0000-000000000202"), WarehouseID: uuid.MustParse("00000000-0000-0000-0000-000000000022"), WarehouseCode: "WH-2", ProductID: productA, Available: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decisions[0].WarehouseCode != "WH-1" {
		t.Fatalf("expected complete product line first, got %+v", result.Decisions)
	}
}

func TestAllocateRejectsInvalidAndInsufficientRequests(t *testing.T) {
	product := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	for _, request := range []AllocationRequest{{}, {Items: []AllocationItem{{ProductID: product, Quantity: 0}}}, {Items: []AllocationItem{{ProductID: product, Quantity: -1}}}, {Items: []AllocationItem{{ProductID: product, Quantity: 1}, {ProductID: product, Quantity: 2}}}} {
		if _, err := Allocate(request, nil); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("request %+v returned %v", request, err)
		}
	}
	if _, err := Allocate(AllocationRequest{Items: []AllocationItem{{ProductID: product, Quantity: 2}}}, []InventoryCandidate{{ProductID: product, Available: 1}}); !errors.Is(err, ErrInsufficientInventory) {
		t.Fatalf("expected insufficient inventory, got %v", err)
	}
}
