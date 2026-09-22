import assert from "node:assert/strict";
import { test } from "node:test";
import { proxyApi } from "../worker/api-proxy.mjs";

const env = { APP_ORIGIN: "https://app.example.com", API_ORIGIN: "https://api.render.example/" };

function upstreamResponse(status, body, cookies = []) {
  const headers = new Headers({ "Content-Type": "application/json", "Cache-Control": "public, max-age=60" });
  for (const cookie of cookies) headers.append("Set-Cookie", cookie);
  return new Response(JSON.stringify(body), { status, headers });
}

test("proxy forwards path, query, body, credentials and preserves separate cookies", async () => {
  let captured;
  const response = await proxyApi(new Request("https://app.example.com/api/v1/orders?limit=1", {
    method: "POST",
    headers: { Accept: "application/json", Authorization: "Bearer access", Cookie: "flowcart_refresh=refresh", Origin: env.APP_ORIGIN, "Content-Type": "application/json" },
    body: JSON.stringify({ item: "one" }),
  }), env, async (request) => {
    captured = request;
    return upstreamResponse(201, { ok: true }, ["flowcart_refresh=one; Path=/; HttpOnly", "flowcart_refresh=two; Path=/; HttpOnly"]);
  });
  assert.equal(captured.url, "https://api.render.example/api/v1/orders?limit=1");
  assert.equal(captured.headers.get("Authorization"), "Bearer access");
  assert.equal(captured.headers.get("Cookie"), "flowcart_refresh=refresh");
  assert.equal(captured.headers.get("Origin"), env.APP_ORIGIN);
  assert.equal(await captured.text(), JSON.stringify({ item: "one" }));
  assert.equal(response.status, 201);
  assert.equal(response.headers.get("Cache-Control"), "no-store");
  assert.deepEqual(response.headers.getSetCookie(), ["flowcart_refresh=one; Path=/; HttpOnly", "flowcart_refresh=two; Path=/; HttpOnly"]);
});

test("proxy forwards refresh and logout cookies without changing auth semantics", async () => {
  const calls = [];
  const fetchImpl = async (request) => {
    calls.push({ path: new URL(request.url).pathname, cookie: request.headers.get("Cookie"), origin: request.headers.get("Origin") });
    return upstreamResponse(200, { access_token: "new-access" }, ["flowcart_refresh=rotated; Path=/; HttpOnly", "flowcart_refresh=; Max-Age=0; Path=/; HttpOnly"]);
  };
  const refresh = await proxyApi(new Request("https://app.example.com/api/v1/auth/refresh", { method: "POST", headers: { Cookie: "flowcart_refresh=old", Origin: env.APP_ORIGIN } }), env, fetchImpl);
  const logout = await proxyApi(new Request("https://app.example.com/api/v1/auth/logout", { method: "POST", headers: { Cookie: "flowcart_refresh=old", Origin: env.APP_ORIGIN } }), env, fetchImpl);
  assert.equal(calls[0].path, "/api/v1/auth/refresh");
  assert.equal(calls[1].path, "/api/v1/auth/logout");
  assert.equal(calls[0].cookie, "flowcart_refresh=old");
  assert.equal(calls[0].origin, env.APP_ORIGIN);
  assert.equal(refresh.headers.getSetCookie().length, 2);
  assert.equal(logout.headers.getSetCookie().length, 2);
  assert.equal(refresh.headers.get("Cache-Control"), "no-store");
});

test("proxy rejects untrusted origins and ignores client upstream input", async () => {
  let called = false;
  const response = await proxyApi(new Request("https://app.example.com/api/v1/auth/refresh?upstream=https://evil.example", { method: "POST", headers: { Origin: "https://evil.example" } }), env, async () => {
    called = true;
    return upstreamResponse(200, {});
  });
  assert.equal(response.status, 403);
  assert.equal(called, false);
});