const forwardedRequestHeaders = ["accept", "content-type", "authorization", "cookie", "idempotency-key", "origin"];

// Match the largest backend body allowance (Stripe webhook). Smaller Go route
// limits still apply upstream. Count actual bytes even without Content-Length.
export const maxProxyBodyBytes = 1 << 20;

async function boundedBody(request) {
  if (!request.body) return undefined;
  const reader = request.body.getReader();
  // One fixed buffer also bounds memory for streams yielding many tiny chunks.
  const body = new Uint8Array(maxProxyBodyBytes);
  let size = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      if (value.byteLength > maxProxyBodyBytes - size) {
        void reader.cancel().catch(() => {});
        return null;
      }
      body.set(value, size);
      size += value.byteLength;
    }
  } finally {
    reader.releaseLock();
  }
  return body.subarray(0, size);
}

function payloadTooLarge() {
  return new Response(JSON.stringify({ error: { code: "payload_too_large", message: "Request body is too large." } }), {
    status: 413,
    headers: { "Content-Type": "application/json", "Cache-Control": "no-store" },
  });
}

function upstreamOrigin(value) {
  const origin = new URL(value);
  if (!['http:', 'https:'].includes(origin.protocol) || origin.username || origin.password || origin.pathname !== '/' || origin.search || origin.hash) {
    throw new Error('API_ORIGIN must be an HTTP(S) origin without credentials, paths, queries or fragments.');
  }
  return origin;
}

function responseHeaders(response) {
  const headers = new Headers();
  response.headers.forEach((value, name) => {
    if (name.toLowerCase() !== 'set-cookie' && name.toLowerCase() !== 'cache-control') headers.set(name, value);
  });
  headers.set('Cache-Control', 'no-store');
  headers.set('Vary', 'Origin');
  const cookies = typeof response.headers.getSetCookie === 'function' ? response.headers.getSetCookie() : [];
  for (const cookie of cookies) headers.append('Set-Cookie', cookie);
  return headers;
}

export async function proxyApi(request, env, fetchImpl = fetch) {
  const requestURL = new URL(request.url);
  if (requestURL.pathname !== '/api' && !requestURL.pathname.startsWith('/api/')) return new Response('Not found', { status: 404 });
  const appOrigin = new URL(env.APP_ORIGIN).origin;
  const requestOrigin = request.headers.get('Origin');
  if (requestOrigin && requestOrigin !== appOrigin) return new Response('Forbidden', { status: 403, headers: { 'Cache-Control': 'no-store' } });

  const upstream = upstreamOrigin(env.API_ORIGIN);
  const target = new URL(`${requestURL.pathname}${requestURL.search}`, upstream);
  const headers = new Headers();
  for (const name of forwardedRequestHeaders) {
    const value = request.headers.get(name);
    if (value !== null) headers.set(name, value);
  }
  const length = request.headers.get('Content-Length');
  if (length !== null && /^\d+$/.test(length) && Number(length) > maxProxyBodyBytes) {
    if (request.body) void request.body.cancel().catch(() => {});
    return payloadTooLarge();
  }
  const body = request.method === 'GET' || request.method === 'HEAD' ? undefined : await boundedBody(request);
  if (body === null) return payloadTooLarge();
  const response = await fetchImpl(new Request(target, { method: request.method, headers, body, redirect: 'manual' }));
  return new Response(response.body, { status: response.status, statusText: response.statusText, headers: responseHeaders(response) });
}

const worker = { fetch: proxyApi };
export default worker;
