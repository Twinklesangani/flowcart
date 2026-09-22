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
Path=/, with expiry/Max-Age. Local HTTP cookies omit Secure. Use same-site HTTPS
frontend/API hosting with this cookie contract; unrelated sites are not supported
by changing CORS alone. NEXT_PUBLIC_API_URL is an API origin, captured at frontend
build time for both requests and CSP. Blank requires same-origin API routing.

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
FLOWCART_SEED_CONFIRM=FLOWCART_DEMO. Blank, staging, production, and typos are
rejected before database connection. Seed business data is unchanged.

S2 continues to use RemoteAddr and ignores X-Forwarded-For/X-Real-IP. Behind a
proxy, its connection IP is shared by clients. Forwarded client IPs must not be
used until an explicit trusted-proxy policy is implemented for the chosen host.

Run Go config/auth/httpboundary/httpapi/seed tests first, then full Go tests with
a disposable database, vet and build. Run frontend lint, TypeScript checking and
a production build; inspect actual production headers and hydration/static assets.
