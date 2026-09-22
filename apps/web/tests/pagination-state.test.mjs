import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import ts from "typescript";

const source = readFileSync(new URL("../src/lib/pagination-state.ts", import.meta.url), "utf8");
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { appendUnique, appendSelectorPage, canLoadMore, isSelectableOptionID, loadMoreOptionID } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

test("inventory pages append unique records and require a usable cursor", () => {
  const first = [{ id: "a" }, { id: "b" }];
  assert.deepEqual(appendUnique(first, [{ id: "b" }, { id: "c" }]), [{ id: "a" }, { id: "b" }, { id: "c" }]);
  assert.equal(canLoadMore({ records: first, has_more: true, next_cursor: "next" }, false), true);
  assert.equal(canLoadMore({ records: first, has_more: true, next_cursor: "next" }, true), false);
  assert.equal(canLoadMore({ records: first, has_more: false }, false), false);
});

test("selector continuation appends uniquely and removes its sentinel at the end", () => {
  const sentinel = () => ({ id: loadMoreOptionID });
  const first = appendSelectorPage([{ id: "a" }], [{ id: "b" }], true, sentinel);
  assert.deepEqual(first.map((item) => item.id), ["a", "b", loadMoreOptionID]);
  const final = appendSelectorPage(first, [{ id: "b" }, { id: "c" }], false, sentinel);
  assert.deepEqual(final.map((item) => item.id), ["a", "b", "c"]);
  assert.equal(isSelectableOptionID(loadMoreOptionID, false), false);
  assert.equal(isSelectableOptionID("c", true), false);
  assert.equal(isSelectableOptionID("c", false), true);
});

test("filter and organization resets cannot retain the prior page cursor", () => {
  const reset = { records: [], next_cursor: undefined, has_more: false };
  assert.deepEqual(reset, { records: [], next_cursor: undefined, has_more: false });
});