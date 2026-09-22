import type { ReactNode } from "react";

export function InventorySections({ inventory, replenishment }: { inventory: ReactNode; replenishment: ReactNode }) {
  return <><section aria-label="Inventory">{inventory}</section><section aria-label="Replenishment">{replenishment}</section></>;
}
