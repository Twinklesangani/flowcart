# Deployment Plan

**Status: FIRST DEMO PREPARATION / PRODUCTION DEPLOYMENT NOT ACTIVE**

This document records the approved architecture and completed checkpoints.
It does not claim hosted production acceptance or security certification.

## Approved architecture

OpenNext + Cloudflare Worker serves the Next.js frontend and its same-origin
/api proxy. The proxy forwards API requests to the Render Free Go/Chi service,
which connects directly to the isolated Neon Free PostgreSQL demo database.
The browser uses the Worker origin for both frontend and API calls.

OpenNext/Worker configuration is present in the repository. The Linux OpenNext
production build is VERIFIED. Cloudflare provisioning and deployment have NOT
STARTED. Vinext is not the selected deployment architecture. Static Cloudflare
Pages export is not selected: dynamic routes such as /orders/[orderID] need the
existing Worker runtime and hosted acceptance.

## Provider progress

| Component | Current checkpoint |
|---|---|
| Neon demo database | PROVISIONED AND VERIFIED; sanitized snapshot and auth/RBAC verified |
| Render | GitHub repository access AVAILABLE; API service NOT CREATED / NOT DEPLOYED |
| Cloudflare frontend | NOT STARTED |
| Production deployment | NOT ACTIVE |

Neon flowcart_demo is the isolated deployment target. Its migration checkpoint
is 15 / false and its approved sanitized data has been restored. Do not rerun
migrations or restore as part of ordinary API startup. The main local flowcart
database remains untouched. Historical incident classification remains UNKNOWN.

## Completed implementation and verification

- S4 — VERIFIED: frontend session isolation and refresh-family protection.
- S5 — VERIFIED: payment reconciliation lifecycle startup/shutdown.
- S6 — VERIFIED: collection and batch-limit hardening; see openapi.yaml.
- S7 — DEFERRED / NOT APPLICABLE: email-based membership onboarding is not implemented.
- R1 — FIXED: bounded Worker request-body buffering.
- R2 — FIXED: TransfersView request-loop and stale-response handling.
- Linux OpenNext production build — VERIFIED.
- Worker bundle feasibility and provider/topology review — completed checkpoints;
  current provider limits must still be checked for the actual deployment.

Verification here records completed scoped checks. It does not substitute for
acceptance on the deployed provider origins.

## Runtime policy and secrets

The first demo uses APP_ENV=production and PAYMENTS_PROVIDER=disabled. Secure
refresh cookies and production security controls stay enabled. No Stripe secrets,
webhooks, or payment reconciliation are configured for this demo. Stripe test
payments require a separate explicitly authorized security task; no production
live-mode exception is permitted.

Render must receive its own DATABASE_URL for the verified direct Neon target,
a fresh strong JWT_SECRET, ACCESS_TOKEN_TTL=15m, and REFRESH_TOKEN_TTL=168h through
provider secret/environment storage. Preserve TLS certificate and hostname
verification. Never use the local development DATABASE_URL or commit credentials.
No production credentials are committed.

Production seeding is rejected before database connection. Only explicit local
development/test/verification environments and allowlisted local targets are
supported by seed. The approved sanitized snapshot supplied the Neon demo data;
private password variables do not enable production seeding. Existing demo
accounts are used without public self-registration or email-based member addition.

## Same-origin Worker proxy and cookies

Build the frontend with NEXT_PUBLIC_API_MODE=proxy and NEXT_PUBLIC_APP_ENV=production.
Configure Worker API_ORIGIN with the actual HTTPS Render origin and APP_ORIGIN
with the exact HTTPS frontend origin. Configure the API CORS_ALLOWED_ORIGINS with
that same frontend origin. Do not invent a Worker hostname or use wildcard CORS.
Until the frontend origin is established, record CORS_ORIGIN_PENDING_CLOUDFLARE
and stop before enabling a publicly reachable production deployment.

The Worker forwards auth requests and Set-Cookie responses. The browser receives
a host-only HttpOnly, Secure, SameSite=Lax, Path=/ refresh cookie on the Worker
origin. Keep the existing Origin checks and cookie policy. Direct cross-site
browser calls from workers.dev to onrender.com are not the approved cookie flow.
Do not weaken SameSite or add a broad cookie Domain as a workaround.

Provider HTTPS domains are the first-demo direction. A custom domain is optional
and requires a separate selection and DNS/TLS verification before use. If custom
domains are later chosen, retain the approved proxy topology and verify the exact
origins again. No custom domain or DNS configuration is complete.

## Remaining deployment gates

- Review the final deployment commit and confirm GitHub contains the approved
  source snapshot before configuring a service or triggering a build.
- Configure Render's existing Go build/start entry point from apps/api, using
  Render's PORT. Keep migrations explicit and seed out of startup.
- Store and verify production/demo secrets only in provider configuration;
  confirm the service targets Neon flowcart_demo with verified TLS.
- Establish the exact HTTPS frontend origin and configure CORS and Worker origins.
- Verify provider TLS termination, HTTPS redirects, and the intended HSTS policy.
- Configure /health and verify HTTP 200 with database ok from the hosted API.
- Review startup/runtime logs for accidental credential exposure.
- Perform hosted browser acceptance: login, expiry/refresh, logout, RBAC,
  session isolation, proxy behavior, and direct refresh of /orders/[orderID].
- Validate Worker bundle/assets against current limits and measure hosted CPU,
  memory, and request behavior; local build success does not prove hosted quotas.
- Recheck current provider pricing, quotas, regions, and free-tier availability.
  Account for Render cold starts/sleep and Neon compute/storage/egress limits,
  including the effect of health checks on database activity. Free feasibility
  is not a guarantee of unlimited or continuously available production hosting.
- Complete provider backup/retention, recovery, and rollback procedures, including
  a recovery rehearsal and an explicit procedure for future schema migrations.
- Resolve any custom-domain/DNS decision separately; verify DNS, TLS, and cookie
  behavior if a custom domain is introduced.
- Complete hosted production security verification without treating earlier local
  or Neon checks as security certification.

## Deferred scope

Email verification and invitation acceptance remain deferred until their security
contract and delivery workflow are defined. Stripe tests remain a separate task.
Redis, additional API providers, and AWS infrastructure are not required for this
approved demo. No Render service or Cloudflare deployment is active.
