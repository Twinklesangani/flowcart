package inventory

import (
	"context"
	"errors"
	"testing"

	"flowcart/apps/api/internal/organization"

	"github.com/google/uuid"
)

func int64Ptr(value int64) *int64 { return &value }

func TestReplenishmentPolicyValidation(t *testing.T) {
	valid := []ReplenishmentPolicyInput{
		{},
		{ReorderPoint: int64Ptr(0), TargetStockLevel: int64Ptr(0)},
		{ReorderPoint: int64Ptr(5), TargetStockLevel: int64Ptr(15)},
	}
	for _, input := range valid {
		if !validReplenishmentPolicy(input) {
			t.Fatalf("policy rejected: %+v", input)
		}
	}
	invalid := []ReplenishmentPolicyInput{
		{ReorderPoint: int64Ptr(5)},
		{TargetStockLevel: int64Ptr(15)},
		{ReorderPoint: int64Ptr(-1), TargetStockLevel: int64Ptr(15)},
		{ReorderPoint: int64Ptr(15), TargetStockLevel: int64Ptr(5)},
	}
	for _, input := range invalid {
		if validReplenishmentPolicy(input) {
			t.Fatalf("policy accepted: %+v", input)
		}
	}
}

func TestReplenishmentRecommendationsDeterministicAndConservative(t *testing.T) {
	productID := uuid.New()
	destination := LowStockItem{InventoryLevel: InventoryLevel{ProductID: productID, WarehouseID: uuid.New(), AvailableQuantity: 3, TargetStockLevel: int64Ptr(15)}}
	donors := []donorCandidate{
		{ProductID: productID, WarehouseID: uuid.New(), WarehouseCode: "B", InventoryLevelID: uuid.New(), EffectiveAvailable: 12, ReorderPoint: 8},
		{ProductID: productID, WarehouseID: uuid.New(), WarehouseCode: "C", InventoryLevelID: uuid.New(), EffectiveAvailable: 10, ReorderPoint: 7},
		{ProductID: productID, WarehouseID: uuid.New(), WarehouseCode: "UNCONFIGURED", InventoryLevelID: uuid.New(), EffectiveAvailable: 0, ReorderPoint: 100},
	}
	result := applyRecommendations([]LowStockItem{destination}, donors, false)[0]
	if result.RecommendedQuantity != 12 || result.UnfulfilledQuantity != 0 {
		t.Fatalf("recommendation = %+v", result)
	}
	if len(result.DonorAllocations) != 1 || result.DonorAllocations[0].Quantity != 12 {
		t.Fatalf("allocations = %+v", result.DonorAllocations)
	}

	destination.TargetStockLevel = int64Ptr(30)
	result = applyRecommendations([]LowStockItem{destination}, donors, false)[0]
	if result.RecommendedQuantity != 22 || result.UnfulfilledQuantity != 5 {
		t.Fatalf("split recommendation = %+v", result)
	}
	if len(result.DonorAllocations) != 2 || result.DonorAllocations[0].Quantity != 12 || result.DonorAllocations[1].Quantity != 10 {
		t.Fatalf("split allocations = %+v", result.DonorAllocations)
	}
}

func TestReplenishmentDonorLimitAtExactLimit(t *testing.T) {
	productID := uuid.New()
	destination := LowStockItem{InventoryLevel: InventoryLevel{ProductID: productID, WarehouseID: uuid.New(), AvailableQuantity: 0, TargetStockLevel: int64Ptr(100)}}
	donors := make([]donorCandidate, replenishmentDonorLimit)
	for index := range donors {
		donors[index] = donorCandidate{ProductID: productID, WarehouseID: uuid.New(), WarehouseCode: "EXACT", InventoryLevelID: uuid.New(), EffectiveAvailable: 20, ReorderPoint: 1}
	}
	result := applyRecommendations([]LowStockItem{destination}, donors, false)[0]
	if result.DonorsTruncated {
		t.Fatal("exact donor limit was marked truncated")
	}
}

func TestReplenishmentDonorLimitPlusOneIsTruncated(t *testing.T) {
	productID := uuid.New()
	destination := LowStockItem{InventoryLevel: InventoryLevel{ProductID: productID, WarehouseID: uuid.New(), AvailableQuantity: 0, TargetStockLevel: int64Ptr(100)}}
	donors := make([]donorCandidate, replenishmentDonorLimit+1)
	for index := range donors {
		donors[index] = donorCandidate{ProductID: productID, WarehouseID: uuid.New(), WarehouseCode: "PLUS-ONE", InventoryLevelID: uuid.New(), EffectiveAvailable: 20, ReorderPoint: 1}
	}
	result := applyRecommendations([]LowStockItem{destination}, donors, false)[0]
	if !result.DonorsTruncated {
		t.Fatal("limit-plus-one donor result was not marked truncated")
	}
}

func TestReplenishmentReadRBACAndBounds(t *testing.T) {
	service := NewService(nil)
	for _, role := range []organization.Role{organization.RoleSupport, organization.RoleViewer} {
		tenant := organization.TenantContext{OrganizationID: uuid.New(), Role: role}
		if _, err := service.UpdateReplenishmentPolicy(context.Background(), tenant, uuid.New(), ReplenishmentPolicyInput{}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("role=%s policy error=%v", role, err)
		}
	}
	if _, err := service.LowStock(context.Background(), organization.TenantContext{}, 101); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("limit error=%v", err)
	}
}
