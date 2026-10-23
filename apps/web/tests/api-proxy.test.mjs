import assert from "node:assert/strict";
import { test } from "node:test";
import { maxProxyBodyBytes, proxyApi } from "../worker/api-proxy.mjs";

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

function streamingRequest(headers = {}) {
  let reads = 0;
  let cancelled = false;
  const body = new ReadableStream({
    pull(controller) { reads++; controller.enqueue(new Uint8Array(64 * 1024)); },
    cancel() { cancelled = true; },
  }, { highWaterMark: 0 });
  return {
    request: new Request("https://app.example.com/api/v1/orders", { method: "POST", headers, body, duplex: "half" }),
    reads: () => reads,
    cancelled: () => cancelled,
  };
}

test("known oversized body is cancelled without reading or contacting upstream", async () => {
  const stream = streamingRequest({ "Content-Length": String(maxProxyBodyBytes + 1) });
  const response = await proxyApi(stream.request, env, () => assert.fail("upstream must not be called"));
  assert.equal(response.status, 413);
  assert.equal((await response.json()).error.code, "payload_too_large");
  assert.equal(response.headers.get("Cache-Control"), "no-store");
  assert.equal(stream.reads(), 0);
  assert.equal(stream.cancelled(), true);
});

for (const headers of [{}, { "Content-Length": "1" }, { "Content-Length": "invalid" }]) {
  test(`actual bytes bound an endless stream with length ${headers["Content-Length"] ?? "unknown"}`, async () => {
    const stream = streamingRequest(headers);
    const response = await proxyApi(stream.request, env, () => assert.fail("upstream must not be called"));
    assert.equal(response.status, 413);
    assert.equal(stream.reads(), maxProxyBodyBytes / (64 * 1024) + 1);
    assert.equal(stream.cancelled(), true);
  });
}

test("the exact maximum body is forwarded byte-for-byte without JSON parsing", async () => {
  const body = new Uint8Array(maxProxyBodyBytes).fill(123);
  const response = await proxyApi(new Request("https://app.example.com/api/v1/webhooks/stripe", { method: "POST", body }), env, async request => {
    assert.deepEqual(new Uint8Array(await request.arrayBuffer()), body);
    return upstreamResponse(200, { ok: true });
  });
  assert.equal(response.status, 200);
});

test("many small chunks preserve the exact payload", async () => {
  let remaining = 2048;
  const body = new ReadableStream({ pull(controller) {
    if (remaining-- > 0) controller.enqueue(new Uint8Array([120]));
    else controller.close();
  } });
  const response = await proxyApi(new Request("https://app.example.com/api/v1/orders", { method: "POST", body, duplex: "half" }), env, async request => {
    assert.equal(await request.text(), "x".repeat(2048));
    return upstreamResponse(200, {});
  });
  assert.equal(response.status, 200);
});

test("GET, HEAD, DELETE and POST without a body remain bodyless", async () => {
  for (const method of ["GET", "HEAD", "DELETE", "POST"]) {
    const response = await proxyApi(new Request("https://app.example.com/api/v1/orders", { method }), env, async request => {
      assert.equal(request.method, method);
      assert.equal(request.body, null);
      return new Response(null, { status: 204 });
    });
    assert.equal(response.status, 204);
  }
});

test("upstream authentication, body-limit and rate-limit errors remain unchanged", async () => {
  for (const status of [401, 403, 413, 429, 503]) {
    const response = await proxyApi(new Request("https://app.example.com/api/v1/orders", {
      method: "POST", body: "{}", headers: {
        Authorization: "Bearer access", Cookie: "flowcart_refresh=refresh", "Idempotency-Key": "operation",
        "Content-Type": "application/json", Origin: env.APP_ORIGIN, "X-Forwarded-For": "untrusted",
      },
    }), env, async request => {
      assert.equal(request.headers.get("Authorization"), "Bearer access");
      assert.equal(request.headers.get("Cookie"), "flowcart_refresh=refresh");
      assert.equal(request.headers.get("Idempotency-Key"), "operation");
      assert.equal(request.headers.get("Origin"), env.APP_ORIGIN);
      assert.equal(request.headers.get("X-Forwarded-For"), null);
      assert.equal(request.redirect, "manual");
      return new Response('{"error":{"code":"upstream_error"}}', { status, headers: { "Retry-After": "7" } });
    });
    assert.equal(response.status, status);
    assert.equal(response.headers.get("Retry-After"), "7");
    assert.equal(await response.text(), '{"error":{"code":"upstream_error"}}');
  }
});
