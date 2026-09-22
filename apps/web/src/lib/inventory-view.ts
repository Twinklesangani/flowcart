import type { InventoryLevel, LowStockItem, DonorAllocation } from "./api";

export function inventoryStatus(item: InventoryLevel): LowStockItem["status"] {
  if (item.reorder_point == null || item.target_stock_level == null) return "unconfigured";
  if (item.available_quantity <= 0) return "out_of_stock";
  return item.available_quantity <= item.reorder_point ? "low" : "healthy";
}

export function replenishmentTransferHref(item: LowStockItem, donor: DonorAllocation): string {
  const query = new URLSearchParams({
    source: donor.source_warehouse_id,
    destination: item.warehouse_id,
    product: item.product_id,
    quantity: String(donor.quantity),
  });
  return `/transfers/new?${query}`;
}
