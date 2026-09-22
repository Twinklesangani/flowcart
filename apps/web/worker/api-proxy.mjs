const forwardedRequestHeaders = ["accept", "content-type", "authorization", "cookie", "idempotency-key", "origin"];

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
  const body = request.method === 'GET' || request.method === 'HEAD' ? undefined : await request.arrayBuffer();
  const response = await fetchImpl(new Request(target, { method: request.method, headers, body, redirect: 'manual' }));
  return new Response(response.body, { status: response.status, statusText: response.statusText, headers: responseHeaders(response) });
}

const worker = { fetch: proxyApi };
export default worker;