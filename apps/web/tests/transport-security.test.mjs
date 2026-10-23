import assert from "node:assert/strict";
import { test } from "node:test";
import { secureWorkerResponse } from "../worker/transport-security.mjs";

test("HTTP requests are redirected to HTTPS without reaching the app", async () => {
  let reachedApp = false;
  const response = await secureWorkerResponse(
    new Request("http://flowcart.example/orders?limit=10"),
    async () => {
      reachedApp = true;
      return new Response("unexpected");
    },
  );

  assert.equal(response.status, 308);
  assert.equal(response.headers.get("Location"), "https://flowcart.example/orders?limit=10");
  assert.equal(reachedApp, false);
});

test("HTTPS responses gain HSTS without overwriting existing security or cookie headers", async () => {
  const response = await secureWorkerResponse(
    new Request("https://flowcart.example/api/v1/auth/refresh"),
    async () => new Response("ok", {
      headers: {
        "Cache-Control": "no-store",
        "Content-Security-Policy": "default-src 'self'",
        "X-Content-Type-Options": "nosniff",
        "Set-Cookie": "flowcart_refresh=rotated; Path=/; HttpOnly; Secure; SameSite=Lax",
      },
    }),
  );

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("Strict-Transport-Security"), "max-age=31536000");
  assert.equal(response.headers.get("Cache-Control"), "no-store");
  assert.equal(response.headers.get("Content-Security-Policy"), "default-src 'self'");
  assert.equal(response.headers.get("X-Content-Type-Options"), "nosniff");
  assert.deepEqual(response.headers.getSetCookie(), ["flowcart_refresh=rotated; Path=/; HttpOnly; Secure; SameSite=Lax"]);
});
