import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

const source = readFileSync(new URL("../src/components/workflow.tsx", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: { jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;

// Exercise the real component with deterministic hook lifecycles, following the
// existing Node test approach. No browser, network, dependency or timer needed.
function mount(request) {
  const slots = [];
  const pendingEffects = [];
  let index = 0;
  let dirty = true;
  let tree;
  let session = { client: { request }, organization: { id: "org-one", role: "viewer" } };
  const changed = (previous, next) => !previous || next.some((value, i) => !Object.is(value, previous[i]));
  const hooks = {
    useState(initial) {
      const position = index++;
      slots[position] ??= { value: typeof initial === "function" ? initial() : initial };
      return [slots[position].value, value => {
        const next = typeof value === "function" ? value(slots[position].value) : value;
        if (!Object.is(next, slots[position].value)) { slots[position].value = next; dirty = true; }
      }];
    },
    useRef(initial) {
      const position = index++;
      slots[position] ??= { value: { current: initial } };
      return slots[position].value;
    },
    useCallback(callback, deps) {
      const position = index++;
      if (changed(slots[position]?.deps, deps)) slots[position] = { value: callback, deps };
      return slots[position].value;
    },
    useEffect(effect, deps) {
      const position = index++;
      const previous = slots[position];
      if (changed(previous?.deps, deps)) {
        slots[position] = { deps, cleanup: previous?.cleanup };
        pendingEffects.push(() => {
          slots[position].cleanup?.();
          slots[position].cleanup = effect();
        });
      }
    },
  };
  const element = (type, props) => ({ type, props: props ?? {} });
  const display = name => Object.assign(() => null, { displayName: name });
  const exports = {};
  const dependencies = {
    react: hooks,
    "react/jsx-runtime": { jsx: element, jsxs: element, Fragment: "Fragment" },
    "next/link": { default: display("Link") },
    "next/navigation": { useRouter: () => ({ push() {} }) },
    "../lib/session": { useSession: () => session, canOperate: () => false },
    "./states": Object.fromEntries(["EmptyState", "ErrorState", "LoadingState", "PermissionDeniedState"].map(name => [name, display(name)])),
    "./operations": { StatusBadge: display("StatusBadge") },
    "../lib/pagination-state": { appendUnique: (previous, next) => [...previous, ...next.filter(item => !previous.some(old => old.id === item.id))] },
    "../lib/transfer-create-controller": {},
    "../lib/request-generation": { isCurrentGeneration: (requestGeneration, current) => requestGeneration === current },
  };
  vm.runInNewContext(compiled, { exports, queueMicrotask, URLSearchParams, require(name) {
    assert.ok(name in dependencies, `unexpected import ${name}`);
    return dependencies[name];
  } });
  return {
    async settle() {
      // A fixed number of microtask turns also exposes the old unbounded loop:
      // rerenders/effects continue firing instead of silently being discarded.
      for (let turn = 0; turn < 40; turn++) {
        if (dirty) {
          dirty = false;
          index = 0;
          tree = exports.TransfersView();
          while (pendingEffects.length) pendingEffects.shift()();
        }
        await Promise.resolve();
      }
    },
    nodes(predicate) {
      const found = [];
      function visit(node) {
        if (Array.isArray(node)) { node.forEach(visit); return; }
        if (!node || typeof node !== "object") return;
        if (predicate(node)) found.push(node);
        visit(node.props?.children);
      }
      visit(tree);
      return found;
    },
    changeOrganization(id) { session = { ...session, organization: { id, role: "viewer" } }; dirty = true; },
    unmount() { slots.forEach(slot => slot.cleanup?.()); },
  };
}

const transfer = id => ({ id, status: "pending", source_warehouse_code: "A", destination_warehouse_code: "B", items: [], created_at: "2026-10-19T00:00:00Z" });
const page = (ids = [], more = false) => ({ transfers: ids.map(transfer), has_more: more, ...(more ? { next_cursor: "next-page" } : {}) });
const moreButton = view => view.nodes(node => node.type === "button" && node.props.className?.includes("pagination-button"))[0];

test("one mount and successful state updates make only one request", async () => {
  const calls = [];
  const view = mount(async path => { calls.push(path); return page(["one"]); });
  await view.settle();
  await view.settle();
  assert.equal(calls.length, 1);
  assert.match(calls[0], /transfers\?limit=50$/);
  assert.equal(view.nodes(node => node.type === "tr" && node.props.children?.[0]?.props?.children?.props?.href === "/transfers/one").length, 1);
  view.unmount();
});

test("pagination sends one cursor request, appends rows and guards double clicks", async () => {
  const calls = [];
  const view = mount(async path => { calls.push(path); return calls.length === 1 ? page(["one"], true) : page(["two"]); });
  await view.settle();
  const button = moreButton(view);
  button.props.onClick();
  button.props.onClick();
  await view.settle();
  await view.settle();
  assert.equal(calls.length, 2);
  assert.equal(new URL(calls[1], "https://test.example").searchParams.get("cursor"), "next-page");
  assert.equal(view.nodes(node => node.type === "tbody")[0].props.children.length, 2);
  assert.equal(moreButton(view), undefined);
  view.unmount();
});

test("status filter reloads once from the first page and resets pagination", async () => {
  const calls = [];
  const view = mount(async path => { calls.push(path); return page([], true); });
  await view.settle();
  view.nodes(node => node.type === "select")[0].props.onChange({ target: { value: "pending" } });
  await view.settle();
  await view.settle();
  assert.equal(calls.length, 2);
  const query = new URL(calls[1], "https://test.example").searchParams;
  assert.equal(query.get("status"), "pending");
  assert.equal(query.has("cursor"), false);
  view.unmount();
});

test("load errors do not retry until the user requests retry", async () => {
  let calls = 0;
  const view = mount(async () => { calls++; throw new Error("unavailable"); });
  await view.settle();
  await view.settle();
  assert.equal(calls, 1);
  view.nodes(node => node.type?.displayName === "ErrorState")[0].props.onRetry();
  await view.settle();
  assert.equal(calls, 2);
  view.unmount();
});

test("forbidden response preserves the permission-denied state without retry", async () => {
  let calls = 0;
  const view = mount(async () => { calls++; throw { apiError: { status: 403 } }; });
  await view.settle();
  assert.equal(calls, 1);
  assert.equal(view.nodes(node => node.type?.displayName === "PermissionDeniedState").length, 1);
  view.unmount();
});

test("changing tenant ignores an older response and loads the intended tenant once", async () => {
  const calls = [];
  let resolveOld;
  const view = mount(path => { calls.push(path); return calls.length === 1 ? new Promise(resolve => { resolveOld = resolve; }) : Promise.resolve(page(["new"])); });
  await view.settle();
  view.changeOrganization("org-two");
  await view.settle();
  resolveOld(page(["old"], true));
  await view.settle();
  assert.equal(calls.length, 2);
  assert.match(calls[1], /organizations\/org-two\/transfers/);
  assert.equal(moreButton(view), undefined);
  assert.equal(view.nodes(node => node.props.href === "/transfers/old").length, 0);
  view.unmount();
});
