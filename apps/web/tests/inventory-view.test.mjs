import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";

const source = readFileSync(new URL("../src/lib/inventory-view.ts", import.meta.url), "utf8");
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } });
const { inventoryStatus, replenishmentTransferHref } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

test("inventory status is determined by its own policy and availability", () => {
  assert.equal(inventoryStatus({ available_quantity: 39 }), "unconfigured");
  assert.equal(inventoryStatus({ available_quantity: 0, reorder_point: null, target_stock_level: null }), "unconfigured");
  const policy = { reorder_point: 5, target_stock_level: 12 };
  assert.equal(inventoryStatus({ ...policy, available_quantity: 0 }), "out_of_stock");
  assert.equal(inventoryStatus({ ...policy, available_quantity: 5 }), "low");
  assert.equal(inventoryStatus({ ...policy, available_quantity: 6 }), "healthy");
});

test("recommendation opens the existing transfer form with the donor route and quantity", () => {
  const url = new URL(replenishmentTransferHref({ warehouse_id: "BNE", product_id: "coffee" }, { source_warehouse_id: "MEL", quantity: 10 }), "http://localhost");
  assert.equal(url.pathname, "/transfers/new");
  assert.deepEqual(Object.fromEntries(url.searchParams), { source: "MEL", destination: "BNE", product: "coffee", quantity: "10" });
});
