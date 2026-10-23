# API proxy body boundary

The proxy accepts at most 1 MiB of actual request-body bytes, matching the Go
Stripe webhook maximum. Smaller Go limits (16 KiB authentication JSON and
64 KiB mutation JSON) remain enforced by their existing handlers. Bodies are
forwarded unchanged, including webhook raw bytes; no JSON parsing occurs here.

An oversized Content-Length can reject a request before reading. Every other
body is read incrementally into one fixed 1 MiB buffer; the proxy stops and
cancels the reader on the first byte over the limit. Missing, invalid or
understated Content-Length cannot bypass that count. No upstream request is
sent until the bounded read succeeds. Bodyless requests remain bodyless.

This deliberately uses bounded buffering rather than forwarding an unchecked
stream: it can return a deterministic 413 before the upstream receives a partial
mutation. It does not promise zero allocation: the Worker runtime allocates
incoming chunks before application code can inspect them, and cancellation
does not guarantee that network bytes already in flight disappear. Application
retained body storage is bounded; platform chunk/Request allocations, concurrency
and slow uploads still consume resources. Cloudflare also enforces its own
plan-specific upload limits and a 128 MB per-isolate memory limit (shared across
concurrent requests), not a separate allowance per request.

References: [Cloudflare request API](https://developers.cloudflare.com/workers/runtime-apis/request/)
and [platform limits](https://developers.cloudflare.com/workers/platform/limits/).

Run focused proxy tests from `apps/web`: `node --test tests/api-proxy.test.mjs`.
