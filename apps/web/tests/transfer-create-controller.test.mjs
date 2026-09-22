import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import ts from "typescript";

const source = readFileSync(new URL("../src/lib/transfer-create-controller.ts", import.meta.url), "utf8");
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { TransferCreateController, transferSelectorError, transferSelectorLoading, transferSelectorSentinel } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

const products = Array.from({ length: 50 }, (_, index) => ({ id: `P${index + 1}`, sku: `P-${index + 1}`, name: `Product ${index + 1}` }));
const warehouses = Array.from({ length: 50 }, (_, index) => ({ id: `W${index + 1}`, code: `W-${index + 1}`, name: `Warehouse ${index + 1}` }));

test("production transfer controller paginates selectors and builds a sanitized second-page payload", async () => {
  const calls = [];
  let submitted;
  const request = async (path, options = {}) => {
    calls.push({ path, options });
    const url = new URL(`http://flowcart.test${path}`);
    if (path.includes("/products?")) {
      return url.searchParams.get("cursor") === "product-page-2"
        ? { products: [{ id: "P50", sku: "P-50", name: "Product 50" }, { id: "P51", sku: "P-51", name: "Product 51" }], has_more: false }
        : { products, next_cursor: "product-page-2", has_more: true };
    }
    if (path.includes("/warehouses?")) {
      return url.searchParams.get("cursor") === "warehouse-page-2"
        ? { warehouses: [{ id: "W50", code: "W-50", name: "Warehouse 50" }, { id: "W51", code: "W-51", name: "Warehouse 51" }, { id: "W52", code: "W-52", name: "Warehouse 52" }], has_more: false }
        : { warehouses, next_cursor: "warehouse-page-2", has_more: true };
    }
    submitted = JSON.parse(options.body);
    return { id: "transfer-proof" };
  };
  const controller = new TransferCreateController(request);
  controller.resetOrganization("org-a");
  await Promise.all([controller.loadProducts(), controller.loadWarehouses()]);
  assert.equal(controller.products.length, 50);
  assert.equal(controller.warehouses.length, 50);
  await controller.select("product", transferSelectorSentinel);
  await controller.select("source", transferSelectorSentinel);
  await controller.select("destination", transferSelectorSentinel);
  assert.ok(calls.some(({ path }) => path.includes("cursor=product-page-2")));
  assert.ok(calls.some(({ path }) => path.includes("cursor=warehouse-page-2")));
  assert.equal(controller.products.filter(({ id }) => id === "P50").length, 1);
  assert.equal(controller.warehouses.filter(({ id }) => id === "W50").length, 1);
  assert.ok(controller.products.some(({ id }) => id === "P51"));
  assert.ok(controller.warehouses.some(({ id }) => id === "W51"));
  assert.ok(controller.warehouses.some(({ id }) => id === "W52"));
  const callsAfterExhaustion = calls.length;
  await controller.select("product", transferSelectorSentinel);
  await controller.select("source", transferSelectorSentinel);
  assert.equal(calls.length, callsAfterExhaustion);
  await controller.select("product", transferSelectorLoading);
  await controller.select("source", transferSelectorError);
  await controller.select("product", "P51");
  await controller.select("source", "W51");
  await controller.select("destination", "W52");
  const payload = controller.buildPayload("2");
  assert.equal(payload.items[0].product_id, "P51");
  assert.equal(payload.source_warehouse_id, "W51");
  assert.equal(payload.destination_warehouse_id, "W52");
  assert.equal(payload.items[0].quantity, 2);
  assert.ok(!JSON.stringify(payload).includes(transferSelectorSentinel));
  await controller.select("destination", "W51");
  assert.throws(() => controller.buildPayload("2"), /must differ/);
  await controller.select("destination", "W52");
  await controller.submit("2");
  assert.deepEqual(submitted, payload);
  controller.resetOrganization("org-b");
  assert.deepEqual(controller.products, []);
  assert.deepEqual(controller.warehouses, []);
  assert.equal(controller.productCursor, "");
  assert.equal(controller.warehouseCursor, "");
  assert.equal(controller.productID, "");
  assert.equal(controller.sourceID, "");
  assert.equal(controller.destinationID, "");
});
