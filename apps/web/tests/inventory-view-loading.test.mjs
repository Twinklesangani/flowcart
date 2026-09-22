import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import ts from "typescript";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);

const source = readFileSync(new URL("../src/components/operations.tsx", import.meta.url), "utf8");
// Mock everything!
const preamble = `
const React = {
  get useState() { return globalThis.React.useState; },
  get useEffect() {
    return (fn, deps) => {
      globalThis.React.useEffect(fn, deps);
    };
  },
  get useCallback() { return globalThis.React.useCallback; },
  get useRef() { return globalThis.React.useRef; },
  createElement: (t, p, ...c) => ({ type: typeof t === 'function' ? t.name : t, props: p, children: c }),
  Fragment: "Fragment"
};
const useState = (...args) => React.useState(...args);
const useEffect = (...args) => React.useEffect(...args);
const useCallback = (...args) => React.useCallback(...args);
const useRef = (...args) => React.useRef(...args);
const useSession = () => globalThis.useSession();
const InventorySections = (props) => props;
const apiStatus = (e) => { console.error("CAUGHT ERROR IN RENDER:", e); return 500; };
const EmptyState = () => null;
const LoadingState = () => null;
const ErrorState = (props) => ({ type: "ErrorState", props });
const PageHeading = () => null;
const StatusBadge = () => null;
const PermissionDeniedState = () => null;
const OrderTimeline = () => null;
const OrderDetailBase = () => null;
const canOperate = () => true;
const replenishmentTransferHref = () => "";
const inventoryStatus = () => "healthy";
const Link = () => null;
const appendUnique = (a, b) => [...a, ...b];
`;
const replaced = preamble + source.replace(/import\s+.*?\s+from\s+['"].*?['"];/g, '');

const { outputText } = ts.transpileModule(replaced, {
  compilerOptions: { jsx: ts.JsxEmit.React, module: ts.ModuleKind.CommonJS }
});

const dir = mkdtempSync(join(process.cwd(), ".operations-test-"));
const modulePath = join(dir, "operations.cjs");
writeFileSync(modulePath, outputText);

globalThis.window = { setTimeout: globalThis.setTimeout, clearTimeout: globalThis.clearTimeout };
const { InventoryView } = require(modulePath);

// To test hooks, we will create a lightweight hook runner!
function runHook(Component) {
  let state = [];
  let stateIdx = 0;
  let effects = [];
  let effectIdx = 0;
  let callbackIdx = 0;
  const effectDeps = [];
  const callbacks = [];
  const callbackDeps = [];

  globalThis.React = {
    useState(initial) {
      const idx = stateIdx++;
      if (state.length <= idx) {
        state.push(typeof initial === 'function' ? initial() : initial);
      }
      const setter = (val) => {
        const next = typeof val === 'function' ? val(state[idx]) : val;
        state[idx] = next;
      };
      return [state[idx], setter];
    },
    useCallback(fn, deps) {
      const idx = callbackIdx++;
      if (!callbacks[idx] || !deps || !callbackDeps[idx] || deps.some((value, index) => value !== callbackDeps[idx][index])) {
        callbacks[idx] = fn;
        callbackDeps[idx] = deps;
      }
      return callbacks[idx];
    },
    useEffect(fn, deps) {
      const idx = effectIdx++;
      if (!deps || !effectDeps[idx] || deps.some((value, index) => value !== effectDeps[idx][index])) {
        effectDeps[idx] = deps;
        effects.push(fn);
      }
    },
    useRef(initial) {
      const idx = stateIdx++;
      if (state.length <= idx) {
        state.push({ current: initial });
      }
      return state[idx];
    },
    createElement: (t, p, ...c) => ({ type: typeof t === 'function' ? t.name : t, props: p, children: c }),
    Fragment: "Fragment"
  };

  const render = () => {
    stateIdx = 0;
    effectIdx = 0;
    callbackIdx = 0;
    effects = [];
    return Component();
  };

  return { render, getEffects: () => effects };
}

test("InventoryView loading path: initial requests settle; Load more preserves appended rows", async () => {
    let requests = [];
    let loadMoreCounter = 0;
    globalThis.useSession = () => ({
        organization: { id: "org1" },
        client: {
            request: async (url) => {
                requests.push(url);
                if (url.includes("low-stock")) {
                    return { inventory: [{ id: "l1" }], next_cursor: "l-next", has_more: true };
                }
                if (loadMoreCounter++ > 0) {
                    return { inventory: [{ id: "i2" }], next_cursor: "i-next2", has_more: false };
                }
                return { inventory: [{ id: "i1" }], next_cursor: "i-next", has_more: true };
            }
        }
    });

    const runner = runHook(InventoryView);
    let element = runner.render();

    // The component should be loading
    assert.equal(element.type, "Fragment");

    // Grab the load function from the effects or from the component closure?
    // Actually load is defined as a useCallback. We can just execute the effect!
    const effect = runner.getEffects()[0];
    effect();
    // This triggers the setTimeout to load
    await new Promise(r => setTimeout(r, 10)); // wait for load
    assert.equal(requests.filter((url) => !url.includes("low-stock")).length, 1);
    assert.equal(requests.filter((url) => url.includes("low-stock")).length, 1);

    // re-render after load
    element = runner.render();
    console.log("TEST 1 MID ELEMENT", JSON.stringify(element, null, 2));
    assert.ok(element.type === "InventorySections" || element.type === "div");
    assert.ok(JSON.stringify(element).includes('"i1"'));
    assert.ok(!JSON.stringify(element).includes('"i2"'));

    // Since we mocked createElement, we can find the onLoadMore prop.
    const findLoadMore = (obj) => {
        if (!obj) return null;
        if (obj.props && obj.props.onLoadMore) return obj.props.onLoadMore;
        if (typeof obj === 'object') {
            for (let k of Object.keys(obj)) {
                const res = findLoadMore(obj[k]);
                if (res) return res;
            }
        }
        return null;
    };
    const loadMore = findLoadMore(element);

    await loadMore();
    await new Promise(r => setTimeout(r, 10)); // wait for load

    element = runner.render();
    // length should be 2 because we append
    assert.ok(JSON.stringify(element).includes('"i1"'));
    assert.ok(JSON.stringify(element).includes('"i2"'));
    assert.equal(requests.filter((url) => !url.includes("low-stock")).length, 2);
    assert.equal(requests.filter((url) => url.includes("low-stock")).length, 1);
});

  function deferred() {
    let resolve;
    const promise = new Promise((done) => { resolve = done; });
    return { promise, resolve };
  }

  async function waitFor(predicate) {
    for (let attempt = 0; attempt < 100; attempt++) {
      if (predicate()) return;
      await new Promise((resolve) => setTimeout(resolve, 0));
    }
    assert.fail("condition did not settle");
  }

  test("InventoryView ignores responses from an older generation", async () => {
    const requests = [];
    globalThis.useSession = () => ({
      organization: { id: "org1" },
      client: {
        request: (url) => {
          const response = deferred();
          requests.push({ url, response });
          return response.promise;
        }
      }
    });

    const runner = runHook(InventoryView);
    runner.render();
    const firstEffect = runner.getEffects()[0];
    firstEffect();
    await waitFor(() => requests.length === 2);

    runner.render();
    const secondEffect = runner.getEffects()[0];
    secondEffect();
    await waitFor(() => requests.length === 4);

    requests[2].response.resolve(requests[2].url.includes("low-stock") ? { inventory: [{ id: "new-low" }], next_cursor: "", has_more: false } : { inventory: [{ id: "new-inventory" }], next_cursor: "", has_more: false });
    requests[3].response.resolve(requests[3].url.includes("low-stock") ? { inventory: [{ id: "new-low" }], next_cursor: "", has_more: false } : { inventory: [{ id: "new-inventory" }], next_cursor: "", has_more: false });
    await waitFor(() => JSON.stringify(runner.render()).includes("new-inventory"));

    requests[0].response.resolve(requests[0].url.includes("low-stock") ? { inventory: [{ id: "old-low" }], next_cursor: "", has_more: false } : { inventory: [{ id: "old-inventory" }], next_cursor: "", has_more: false });
    requests[1].response.resolve(requests[1].url.includes("low-stock") ? { inventory: [{ id: "old-low" }], next_cursor: "", has_more: false } : { inventory: [{ id: "old-inventory" }], next_cursor: "", has_more: false });
    await new Promise((resolve) => setTimeout(resolve, 10));
    const element = runner.render();
    const serialized = JSON.stringify(element);
    assert.match(serialized, /new-inventory/);
    assert.doesNotMatch(serialized, /old-inventory/);
  });

test("InventoryView each initial request failing independently while the other section remains usable", async () => {
    // 1. Inventory fails, low-stock succeeds
    globalThis.useSession = () => ({
        organization: { id: "org1" },
        client: {
            request: async (url) => {
                if (url.includes("low-stock")) {
                    return { inventory: [{ id: "l1" }], next_cursor: "", has_more: false };
                }
                return Promise.reject(new Error("Inventory failed"));
            }
        }
    });

    let runner = runHook(InventoryView);
    let element = runner.render();
    let effect = runner.getEffects()[0];
    effect();
    await new Promise(r => setTimeout(r, 10));

    element = runner.render();
    console.log("TEST 2 ELEMENT", JSON.stringify(element, null, 2));
    assert.ok(element);
    assert.ok(JSON.stringify(element).includes('"continuationError":true') || JSON.stringify(element).includes("ErrorState"));

    // 2. Low-stock fails, inventory succeeds
    globalThis.useSession = () => ({
        organization: { id: "org1" },
        client: {
            request: async (url) => {
                if (url.includes("low-stock")) {
                    return Promise.reject(new Error("Low stock failed"));
                }
                return { inventory: [{ id: "i1" }], next_cursor: "", has_more: false };
            }
        }
    });

    runner = runHook(InventoryView);
    runner.render();
    effect = runner.getEffects()[0];
    effect();
    await new Promise(r => setTimeout(r, 10));

    element = runner.render();
    assert.ok(element);
    // inventory should NOT have continuationError
    assert.ok(!JSON.stringify(element).includes('"continuationError":true') && !JSON.stringify(element).includes("ErrorState"));
    assert.ok(JSON.stringify(element).includes('"lowStockError":true') || JSON.stringify(element).includes("could not be loaded") || JSON.stringify(element).includes("error"));
});

process.on("exit", () => rmSync(dir, { recursive: true, force: true }));
