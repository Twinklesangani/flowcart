import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { createRequire } from "node:module";
import test from "node:test";
import ts from "typescript";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";

const source = readFileSync(new URL("../src/components/inventory-sections.tsx", import.meta.url), "utf8");
const { outputText } = ts.transpileModule(source, { compilerOptions: { jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.CommonJS } });
const directory = mkdtempSync(join(process.cwd(), ".inventory-sections-"));
const modulePath = join(directory, "inventory-sections.cjs");
writeFileSync(modulePath, outputText);
const { InventorySections } = createRequire(import.meta.url)(modulePath);
process.on("exit", () => rmSync(directory, { recursive: true, force: true }));

function render(inventory, replenishment) {
  return renderToStaticMarkup(React.createElement(InventorySections, { inventory, replenishment }));
}

test("production inventory sections render both paginated panels", () => {
  const html = render(
    React.createElement("div", null, "Inventory table", React.createElement("button", null, "Inventory Load more")),
    React.createElement("div", null, "Replenishment queue", React.createElement("button", null, "Replenishment Load more")),
  );
  assert.match(html, /Inventory table/);
  assert.match(html, /Inventory Load more/);
  assert.match(html, /Replenishment queue/);
  assert.match(html, /Replenishment Load more/);
  assert.equal((html.match(/aria-label="Inventory"/g) ?? []).length, 1);
  assert.equal((html.match(/aria-label="Replenishment"/g) ?? []).length, 1);
});

test("production sections keep one section visible when the other reports an error", () => {
  const html = render(React.createElement("div", null, "Inventory error"), React.createElement("div", null, "Replenishment queue"));
  assert.match(html, /Inventory error/);
  assert.match(html, /Replenishment queue/);
});