import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";

async function load(name) {
  const { outputText } = ts.transpileModule(readFileSync(new URL(`../src/lib/${name}.ts`, import.meta.url), "utf8"), { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } });
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);
}
const { ApiClient } = await load("api");
const { SessionController, logoutStorageKey } = await load("session-controller");
const user = { id: "user", first_name: "Test" };
const org = { id: "org-a", name: "A", role: "owner" };
const response = (body, status = 200) => new Response(body === null ? null : JSON.stringify(body), { status });
function deferred() { let resolve; const promise = new Promise((done) => { resolve = done; }); return { promise, resolve }; }
function storage(initial = null) {
  let marker = initial;
  const writes = [];
  return { read: () => marker, write: (value) => { marker = value; writes.push(value); }, clear: () => { marker = null; }, writes };
}
async function until(predicate) {
  for (let i = 0; i < 100; i++) { if (predicate()) return; await new Promise(setImmediate); }
  assert.fail("state transition did not complete");
}
function network(t, custom = () => undefined) {
  const original = globalThis.fetch;
  const calls = [];
  globalThis.fetch = async (path, options) => {
    calls.push({ path, options });
    const result = custom(path, options);
    if (result !== undefined) return result;
    if (path.endsWith("/me")) return response(user);
    if (path.endsWith("/organizations")) return response({ organizations: [org] });
    if (path.endsWith("/logout")) return response(null, 204);
    return response({ access_token: "access-secret" });
  };
  t.after(() => { globalThis.fetch = original; });
  return calls;
}

test("Strict Mode setup-cleanup-setup shares refresh and preserves its valid result", async (t) => {
  const refresh = deferred();
  const calls = network(t, (path) => path.endsWith("/refresh") ? refresh.promise : undefined);
  const controller = new SessionController(new ApiClient(), storage());
  const cleanup = controller.restore(); cleanup(); controller.restore();
  assert.equal(calls.length, 1);
  refresh.resolve(response({ access_token: "restored" }));
  await until(() => controller.getSnapshot().status === "authenticated");
  assert.equal(controller.getSnapshot().organization.id, org.id);
  assert.equal(controller.getSnapshot().accessToken, "restored");
});

for (const order of ["refresh-first", "logout-first"]) {
  test(`late refresh cannot restore state: ${order}`, async (t) => {
    const refresh = deferred(), logout = deferred();
    const calls = network(t, (path) => path.endsWith("/refresh") ? refresh.promise : path.endsWith("/logout") ? logout.promise : undefined);
    const saved = storage();
    const controller = new SessionController(new ApiClient(), saved);
    controller.restore();
    const done = controller.signOut();
    assert.ok(saved.read());
    assert.equal(controller.getSnapshot().status, "signed_out");
    if (order === "refresh-first") {
      refresh.resolve(response({ access_token: "late-secret" }));
      await new Promise(setImmediate);
      logout.resolve(response(null, 204));
    } else {
      logout.resolve(response(null, 204)); await done;
      refresh.resolve(response({ access_token: "late-secret" }));
    }
    await done; await new Promise(setImmediate);
    const state = controller.getSnapshot();
    assert.equal(state.accessToken, null); assert.equal(state.user, null);
    assert.equal(state.organization, null); assert.deepEqual(state.organizations, []);
    assert.equal(calls.filter((c) => c.path.endsWith("/me")).length, 0);
    const reloaded = new SessionController(new ApiClient(), saved); reloaded.restore();
    assert.equal(reloaded.getSnapshot().status, "signed_out");
    assert.equal(calls.filter((c) => c.path.endsWith("/refresh")).length, 1);
  });
}

test("failed logout clears all state immediately and prevents refresh after reload", async (t) => {
  const logout = deferred();
  const calls = network(t, (path) => path.endsWith("/logout") ? logout.promise : undefined);
  const saved = storage();
  const controller = new SessionController(new ApiClient(), saved);
  await controller.signIn("login", {});
  const done = controller.signOut();
  assert.equal(controller.getSnapshot().status, "signed_out");
  assert.equal(controller.getSnapshot().organization, null);
  logout.resolve(response(null, 503)); await done;
  assert.match(controller.getSnapshot().sessionNotice, /revocation could not be confirmed/);
  const reload = new SessionController(new ApiClient(), saved); reload.restore();
  assert.equal(reload.getSnapshot().status, "signed_out");
  assert.equal(calls.filter((c) => c.path.endsWith("/refresh")).length, 0);
  assert.equal(saved.writes.length, 1);
  assert.ok(!saved.writes.some((value) => value.includes("secret")));
});

test("only successful explicit login clears suppression; ordinary hydration and failed login do not", async (t) => {
  let loginFails = true;
  network(t, (path) => path.endsWith("/login") && loginFails ? response(null, 401) : undefined);
  const saved = storage("logout-marker");
  const controller = new SessionController(new ApiClient(), saved);
  controller.restore(); assert.equal(saved.read(), "logout-marker");
  await assert.rejects(controller.signIn("login", {})); assert.equal(saved.read(), "logout-marker");
  loginFails = false; await controller.signIn("login", {});
  assert.equal(saved.read(), null); assert.equal(controller.getSnapshot().status, "authenticated");
  assert.deepEqual(saved.writes, []);
});

test("storage logout invalidates another tab and pending organization hydration", async (t) => {
  const organizations = deferred();
  network(t, (path) => path.endsWith("/organizations") ? organizations.promise : undefined);
  const saved = storage(), second = new SessionController(new ApiClient(), saved);
  const signin = second.signIn("login", {});
  await until(() => second.getSnapshot().accessToken !== null);
  saved.write("other-tab-logout"); second.onStorage({ key: logoutStorageKey, newValue: saved.read() });
  organizations.resolve(response({ organizations: [org] }));
  await assert.rejects(signin);
  assert.equal(second.getSnapshot().status, "signed_out");
  assert.equal(second.getSnapshot().organization, null);
  saved.clear(); second.onStorage({ key: logoutStorageKey, newValue: null });
  assert.equal(second.getSnapshot().status, "signed_out");
});

test("failed refresh expires the session and retries no more than once", async (t) => {
  let failRefresh = false;
  const calls = network(t, (path) => path === "/protected" || (path.endsWith("/refresh") && failRefresh) ? response(null, 401) : undefined);
  const client = new ApiClient(), controller = new SessionController(client, storage());
  await controller.signIn("login", {});
  failRefresh = true; await assert.rejects(client.request("/protected"));
  assert.equal(controller.getSnapshot().status, "signed_out");
  assert.equal(calls.filter((c) => c.path.endsWith("/refresh")).length, 1);
});

test("login waits for old refresh and logout responses before issuing new credentials", async (t) => {
  const refresh = deferred(), logout = deferred();
  const calls = network(t, (path) => path.endsWith("/refresh") ? refresh.promise : path.endsWith("/logout") ? logout.promise : undefined);
  const controller = new SessionController(new ApiClient(), storage());
  controller.restore(); const out = controller.signOut(); const signin = controller.signIn("login", {});
  logout.resolve(response(null, 204)); await out; await new Promise(setImmediate);
  assert.equal(calls.filter((c) => c.path.endsWith("/login")).length, 0);
  refresh.resolve(response({ access_token: "obsolete" })); await signin;
  assert.equal(controller.getSnapshot().status, "authenticated");
  assert.equal(controller.getSnapshot().accessToken, "access-secret");
});

test("successful old organization responses are rejected after logout", async (t) => {
  const old = deferred(); network(t, (path) => path === "/org-a/inventory" ? old.promise : undefined);
  const client = new ApiClient(), controller = new SessionController(client, storage());
  await controller.signIn("login", {});
  const request = client.request("/org-a/inventory");
  await controller.signOut(); old.resolve(response({ inventory: ["old"] }));
  await assert.rejects(request); assert.equal(controller.getSnapshot().organization, null);
});
