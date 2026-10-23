# S3 production configuration

APP_ENV accepts development, test, verification, or production (case normalized,
surrounding whitespace trimmed). Blank defaults to development for local commands;
deployments must explicitly set production. Unknown nonempty modes fail startup.

Production JWT_SECRET must be at least 32 bytes and must not be an obvious
placeholder, repeated-character value, or known development/default secret.
Length and character checks cannot prove entropy. Generate 64 random bytes with
the PowerShell RandomNumberGenerator flow in development.md and store the base64
value in a secret manager. Never use example values or print secrets in logs.
Development/test/verification retain their existing nonempty-secret requirement.

ACCESS_TOKEN_TTL and REFRESH_TOKEN_TTL must be positive Go durations; refresh must
exceed access. Defaults remain 15m and 168h. Signed access JWTs now require exp,
HS256, access type, and a valid UUID subject. No new token format was introduced.

CORS_ALLOWED_ORIGINS is a comma-separated list of exact frontend origins.
Development/test/verification default to http://localhost:3000 and :3001 when
unset. Production requires an explicit list of HTTPS origins, for example
https://operations.example.com. Wildcards, userinfo, non-root paths, queries,
fragments, and invalid ports are rejected. Host case and default ports normalize;
an optional root slash is accepted. Non-default ports remain distinct.
The same immutable policy serves CORS and register/login/refresh/logout checks.
Browser Origin values must match it; absent Origin remains allowed for CLI clients.

Production refresh cookies remain host-only, HttpOnly, Secure, SameSite=Lax,
Path=/, with expiry/Max-Age. Local HTTP cookies omit Secure. The approved demo uses
the OpenNext/Cloudflare Worker's same-origin /api proxy to Render. Browser calls
stay on the Worker origin, which receives the forwarded host-only cookie.
Use NEXT_PUBLIC_API_MODE=proxy and NEXT_PUBLIC_APP_ENV=production. The Worker
requires exact HTTPS API_ORIGIN and APP_ORIGIN values; the API requires that same
frontend origin in CORS_ALLOWED_ORIGINS. Direct cross-site browser calls cannot
be made compatible with SameSite=Lax merely by changing CORS. Hosted cookie and
proxy behavior still require acceptance on the actual provider origins.

The API sends nosniff, no-referrer, and DENY framing headers. Auth no-store and
transient checkout client-secret no-store are preserved. Next.js sends nosniff,
strict-origin-when-cross-origin, DENY, a CSP, and disables unused camera,
microphone and geolocation permissions. No static asset cache policy is changed.
TLS termination, HTTPS redirects, and HSTS belong to the eventual hosting layer.

The CSP permits connections only to self and the configured API origin; local
HMR websocket origins are added in development. Stripe.js is not used and has no
preemptive allowlist. Production excludes unsafe-eval. Current static App Router
HTML contains inline hydration scripts, so script-src retains unsafe-inline as
documented by the installed Next.js guide's non-nonce configuration. This is a
compatibility compromise, not a strict nonce CSP. Nonces would require dynamic
rendering and are outside this configuration-only pass. Development additionally
permits eval for tooling. Inline styles are allowed in production because the
generated Next.js 404/global-error HTML uses them; external styles remain self-only.

Seed requires an explicitly set development/test/verification environment plus
FLOWCART_SEED_CONFIRM=FLOWCART_DEMO and an allowlisted local database target.
Blank, staging, production, and typos are rejected before database connection.
The target must be loopback/localhost with a database name allowed for that
environment. Private password variables do not enable production seeding.
Allowed local environments retain documented development demo defaults; the seed
does not print passwords or overwrite existing account passwords. The isolated
Neon demo database was populated from the verified sanitized snapshot, not by a
production seed command. Do not seed it or rerun its completed migration/restore.

S2 continues to use RemoteAddr and ignores X-Forwarded-For/X-Real-IP. Behind a
proxy, its connection IP is shared by clients. Forwarded client IPs must not be
used until an explicit trusted-proxy policy is implemented for the chosen host.

Run Go config/auth/httpboundary/httpapi/seed tests first, then full Go tests with
a disposable database, vet and build. Run frontend lint, TypeScript checking and
a production build; inspect actual production headers and hydration/static assets.

## Verification status and remaining deployment gates

- IMPLEMENTED: production configuration, Secure cookies, refresh-family
  containment, seed target restrictions, mutation limits, and Stripe live-mode
  enforcement. Demo policy is APP_ENV=production and PAYMENTS_PROVIDER=disabled;
  no Stripe credentials or reconciliation are required for this demo.
- VERIFIED: S4 session isolation/refresh-family protection, S5 reconciliation
  lifecycle, and S6 collection/batch limits. R1 Worker buffering and R2 TransfersView
  request-loop fixes are complete. Linux OpenNext build and the isolated Neon
  snapshot/authentication/RBAC checks are verified checkpoints.
- DEPLOYMENT VERIFICATION REQUIRED: provider secret storage, exact HTTPS origins,
  TLS, health checks, log redaction, actual Worker proxy/cookie/browser behavior,
  and backup/recovery/rollback. Render service is not created; Cloudflare frontend
  is not started. Hosted production security is not yet verified.
- DEFERRED: S7 email verification/invitation and membership onboarding, which are
  not implemented and are not applicable to this demo's existing-account workflow.

No production credentials are committed. Verification describes scoped checks;
it is not a security certification. Production deployment is not active.
