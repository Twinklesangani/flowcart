# Deployment Plan

**Status: PLANNED / NOT YET DEPLOYED**

Public hosting is intentionally blocked until the remaining security gate is
complete. This document records options, not an active deployment.

## Current App Assessment

The web package currently uses Next.js `16.3.4`, React `19.2.8`, and the App
Router. Pages are mostly static shells with client-side API requests; the
session provider is client-side and the API owns the `HttpOnly` refresh cookie.
There are no current `cookies()`, `headers()`, Server Actions, route handlers,
or server-only database calls in `apps/web`.

Cloudflare's current recommendation is **vinext**, but vinext is beta and is a
Vite reimplementation of the Next.js API surface. The compatibility command
for this existing app is `npx vinext check`; that check has not been run here.
**OpenNext for Cloudflare** is the more conservative adapter for preserving
the existing Next build. Its documentation supports all minor and patch
versions of Next 16, App Router, dynamic routes, SSR, and Node.js runtime
behavior. Neither adapter has been installed or configured in this worktree.

Decision: evaluate vinext first in a branch or disposable CI job because it is
Cloudflare's recommended path; retain OpenNext as the fallback if the
compatibility check or browser suite finds a vinext gap. Do not deploy either
until a Linux build and production-mode browser run pass.

## Provisional Architecture

```text
Cloudflare Workers + vinext (OpenNext fallback)
  -> Next.js frontend at app.example.com
  -> HTTPS API origin at api.example.com

Render Free
  -> Go/Chi API
  -> managed secret configuration

Neon Free
  -> PostgreSQL database
```

This is a design target only. No provider account, domain, DNS record, or
hosting resource has been created.

## Frontend feasibility and plans

Cloudflare Pages static export is not selected. The dynamic
`/orders/[orderID]` route and runtime API requests mean an export would not be
a transparent deployment of the current app.

### A) Existing custom domain

Use `app.example.com` for the Worker and `api.example.com` for the Go API.
Keep the API on a separately managed HTTPS service and point only the app host
at Cloudflare Workers. Configure the API with the exact app origin in
`CORS_ALLOWED_ORIGINS`; configure `NEXT_PUBLIC_API_URL` as the API origin;
then run the existing Playwright suite against those two HTTPS hosts.

The refresh cookie is host-only on `api.example.com`, `HttpOnly`, `Secure`,
`SameSite=Lax`, and `Path=/`. `app.example.com` and `api.example.com` are
different origins but the same site, so credentialed `fetch` sends the cookie
to the API when the API response sets it. Do not add a broad `Domain` attribute
unless there is a demonstrated need. Origin checks and CORS still apply.

### B) No purchased domain, strictly $0

Use the platform-provided HTTPS hosts only after verifying their cookie
relationship. A Worker `*.workers.dev` host and an independently hosted API
host such as `*.onrender.com` are different sites, not merely different
origins. The current `SameSite=Lax` host-only refresh cookie therefore cannot
be relied on for the browser's cross-site `fetch` refresh flow.

The strictly $0 plan is consequently **not proven for this architecture**.
Do not weaken the cookie to `SameSite=None` as an unverified workaround: it
requires `Secure`, changes the CSRF threat model, and still needs explicit CORS
and browser testing. The no-cost options are either to keep app and API under
one provider-controlled site boundary, or to change the auth architecture to a
same-origin reverse proxy/BFF; both require a provider-specific proof run.
Without that proof, use local development only or purchase/use a domain for
the same-site subdomain plan.

### Proof still required

- Run `npx vinext check` and a Linux production build; compare the result with
  `npx @opennextjs/cloudflare` and its Worker preview.
- Verify Worker bundle size against the Cloudflare free-plan limit.
- Browser-test login, refresh after access-token expiry, logout, and direct
  refresh of `/orders/[orderID]` on the actual chosen hosts.
- Verify provider free-tier quotas, custom-domain availability, and API
  hosting/database pricing. These cannot be proven from this local worktree.

## Hosted cookie and origin design

Use a custom same-site pair such as `app.example.com` and `api.example.com`.
The refresh cookie stays host-only on the API origin, `HttpOnly`, `Secure`,
`SameSite=Lax`, and `Path=/`. The browser sends it through credentialed fetches
because the two origins are cross-origin but same-site. The API must allow only
the exact frontend origin through `CORS_ALLOWED_ORIGINS`, and the existing
authentication Origin checks remain enabled. Unrelated domains such as
`pages.dev` plus `onrender.com` are not selected for the final cookie flow:
they are cross-site and would not reliably carry the Lax refresh cookie.

## Requirements Before Hosting

- Complete S4 frontend session isolation.
- Complete S5 payment reconciliation lifecycle startup work.
- Complete S6 collection and batch limits.
- Complete S7 email/membership security decision.
- Run final security verification against production-like configuration.
- Configure exact HTTPS CORS origins and secure refresh cookies.
- Store `JWT_SECRET` and Stripe test/live credentials only in a secret manager.
- Add database backup, migration, health, and rollback procedures.

## Hosting Tradeoffs

- **Cloudflare Workers + vinext:** Cloudflare's recommended beta path; requires
  a compatibility check and may expose differences from Next.js runtime
  behavior.
- **Cloudflare Workers + OpenNext:** supports this Next 16 App Router app and
  is the fallback with less migration pressure; still requires adapter
  configuration, Linux validation, account setup, and custom-domain testing.
- **Render Free:** simple API hosting, but cold starts, sleep behavior, and
  connection limits require testing.
- **Neon Free:** removes local database maintenance, but branch recovery,
  connection pooling, retention, and limits require provider verification.
- **AWS compute:** more control and a stronger production story, but more setup
  and operational responsibility.
- **Neon or another managed PostgreSQL service:** removes database maintenance,
  but connection pooling, storage limits, and regional latency must be tested.

No provider account, deployment credential, public URL, DNS record, or external
service is connected by this plan. Free-tier availability and pricing are not
verified.

## Production Provisioning

Public self-registration and email-based membership addition are disabled in
production. Portfolio accounts must be provisioned through the controlled
operator or demo process. Email verification and invitation acceptance are
intentionally deferred until a verified delivery workflow exists.
