import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";

const { outputText } = ts.transpileModule(readFileSync(new URL("../src/lib/api.ts", import.meta.url), "utf8"), { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
const { ApiClient } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

test("overlapping session restorations share one refresh and allow later refreshes", async () => {
  const original = globalThis.fetch;
  let calls = 0;
  let finish;
  globalThis.fetch = async () => { calls++; return new Promise(resolve => { finish = () => resolve(new Response(JSON.stringify({ access_token: "fresh" }))); }); };
  try {
    const client = new ApiClient();
    const first = client.refreshSession();
    const second = client.refreshSession();
    assert.equal(calls, 1);
    finish();
    assert.deepEqual(await Promise.all([first, second]), [{ access_token: "fresh" }, { access_token: "fresh" }]);
    const later = client.refreshSession();
    assert.equal(calls, 2);
    finish();
    await later;
  } finally { globalThis.fetch = original; }
});

test("logout invalidates an in-flight refresh before the stale request retries", async () => {
  const original = globalThis.fetch;
  let calls = 0;
  let finishRefresh;
  globalThis.fetch = async () => {
    calls++;
    if (calls === 1) return new Response(null, { status: 401 });
    return new Promise(resolve => { finishRefresh = () => resolve(new Response(JSON.stringify({ access_token: "stale" }))); });
  };
  try {
    const client = new ApiClient();
    client.setSession("old-token");
    const request = client.request("/protected");
    while (!finishRefresh) await new Promise(resolve => setImmediate(resolve));
    client.invalidateSession();
    finishRefresh();
    await assert.rejects(request);
    assert.equal(calls, 2);
  } finally { globalThis.fetch = original; }
});
