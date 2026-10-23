# FlowCart OS

FlowCart OS is a multi-tenant, multi-warehouse order and fulfillment platform.
It combines inventory correctness, payments, warehouse movement, and a usable
operations frontend in a deliberately understandable modular monolith.

The project models the workflows that make warehouse software difficult:
reservations must not oversell, orders can span warehouses, payment retries
must be safe, and operators need a clear history of what happened.

[Portfolio description](docs/portfolio.md) · [Interview demo](docs/interview-demo.md) · [Deployment plan](docs/deployment-plan.md)

## Engineering Highlights

- PostgreSQL row locks prevent overselling under concurrent workflows.
- Deterministic multi-warehouse allocation preserves reservation correctness.
- Idempotent orders, payments, webhooks, and transfers tolerate retries.
- Trusted provider events reconcile payment and reservation state transactionally.
- Warehouse transfers conserve stock from dispatch through receipt.
- Append-only audit events power operational timelines.
- Organization membership is the source of truth for tenant isolation and RBAC.
- Next.js operations screens consume typed APIs without storing access tokens.

## See It

The seeded Docker demo produces these real application views:

![Operations dashboard](docs/screenshots/dashboard.png)

![Orders and fulfillment](docs/screenshots/orders.png)

![Inventory and replenishment](docs/screenshots/inventory.png)

## Architecture

```mermaid
flowchart TD
    Browser[Browser] --> Web[Next.js frontend]
    Web --> API[Go Chi API]
    API --> Handler[Handler]
    Handler --> Service[Service]
    Service --> Repository[Repository]
    Repository --> DB[(PostgreSQL)]
    Service -. provider calls .-> Stripe[Stripe adapter/webhooks]
    Stripe -. verified events .-> API
```

The backend flow is `handler -> service -> repository -> PostgreSQL`. There are
no Redis, Kafka, Kubernetes, or microservice dependencies in the current build.

## Domain Workflow

```mermaid
flowchart TD
    Order --> Allocation --> Reservation --> Payment --> Committed[Committed stock]
    Committed --> Fulfillment --> Consumption[Inventory consumption]
    LowStock[Low stock] --> Recommendation --> Transfer --> Dispatch --> Receive
```

## Stack

- Next.js 16, React, TypeScript, Tailwind CSS
- Go, Chi, pgx/pgxpool
- PostgreSQL with explicit migrations through `000015`
- Argon2id passwords, JWT access tokens, HttpOnly refresh cookies
- Optional Stripe provider and verified webhooks

## Local Setup

Prerequisites: Docker Desktop, Go, Node.js/npm, and Windows PowerShell.

```powershell
docker compose up -d postgres

cd apps\api
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable"
$env:APP_ENV="development"
go run ./cmd/migrate up
$env:FLOWCART_SEED_CONFIRM="FLOWCART_DEMO"
go run ./cmd/seed
$env:PORT="8081"
$env:JWT_SECRET="replace-with-a-long-development-secret"
go run ./cmd/server
```

In another terminal:

```powershell
cd apps\web
npm ci
npm run dev
```

Open `http://localhost:3000`. The API health endpoint is
`http://localhost:8081/health`. Stop PostgreSQL with `docker compose down`; do
not use `docker compose down -v` unless intentionally deleting the volume.
If port `8081` is already occupied, choose another local API port and set
`NEXT_PUBLIC_API_URL` to the matching URL before starting the frontend.

## Demo Seed

The demo seed creates `FlowCart Demo Retail` with Melbourne,
Sydney, and Brisbane warehouses; 14 products; healthy, low, out-of-stock, and
unconfigured inventory; orders; payments; fulfillment; transfers; and audit
history. It is deterministic and safe to rerun for the demo organization.

Local development/test/verification accounts use the password `FlowCart-Demo-Only-2026!`.

| Role              | Email                 |
| ----------------- | --------------------- |
| Owner             | owner@flowcart.demo   |
| Warehouse manager | manager@flowcart.demo |
| Viewer            | viewer@flowcart.demo  |

**DEMO ONLY - NEVER USE IN PRODUCTION.**

Seeding requires explicit development/test/verification mode, confirmation, and
an allowlisted local database target. Production is rejected before connecting;
private password variables do not enable production seeding. The isolated Neon
demo database contains the separately verified sanitized snapshot. Do not seed it.
See [production security](docs/production-security.md). Hosted owner and manager
credentials remain private.

The seed is deterministic demo data, not a backup. It does not reproduce
manually created orders, transfers, or later workflow history. See
[docs/backup-restore.md](docs/backup-restore.md) for disposable recovery proof.

## API and deployment

The public API contract is documented in [docs/openapi.yaml](docs/openapi.yaml).
The approved demo architecture is OpenNext + Cloudflare Worker for the Next.js
frontend and same-origin /api proxy, Render Free for the Go API, and Neon Free
for PostgreSQL. Browser API calls stay on the Worker origin; the Worker forwards
them to Render and preserves the host-only Secure refresh cookie. Exact HTTPS
CORS and Origin checks remain required. Static export is not used because the
App Router includes dynamic routes such as /orders/[orderID].

Current checkpoints:

- Linux OpenNext production build: VERIFIED.
- Neon demo database: PROVISIONED AND VERIFIED, including sanitized data and auth/RBAC.
- Render GitHub repository access: AVAILABLE; API service: NOT CREATED / NOT DEPLOYED.
- Cloudflare frontend: NOT STARTED.
- Production deployment: NOT ACTIVE.

No production credentials are committed. These checkpoints do not constitute
security certification or hosted production acceptance. Exact frontend origin,
provider secrets, TLS, health checks, hosted browser acceptance, recovery/rollback,
and current pricing/quotas remain deployment gates. Custom domain/DNS configuration
requires a separate decision; provider domains are the first-demo direction.
See [deployment plan](docs/deployment-plan.md).

## Testing

Backend tests include unit tests and real PostgreSQL integration tests for
tenant isolation, reservations, allocation, payments, fulfillment, transfers,
and concurrency-sensitive stock operations. Frontend validation includes lint,
TypeScript checking, production build, and route smoke checks.

```powershell
cd apps\api
go test ./... -count=1
go vet ./...
go build ./...

cd ..\web
npm run lint
npx tsc --noEmit
npm run build
```

## Project Structure

```text
apps/api/cmd/{migrate,seed,server}
apps/api/internal/{auth,organization,product,warehouse,inventory,order,payment,fulfillment,transfer,audit}
apps/api/migrations/000001-000015
apps/web/src/app
apps/web/src/components
apps/web/src/lib
.github/workflows/ci.yml
docker-compose.yml
docs/
```

## Consistency and Tradeoffs

FlowCart favors a modular monolith and PostgreSQL transactions over premature
distributed infrastructure. Integer minor units protect money calculations;
row locking protects stock; idempotency keys protect retries; tenant-scoped
queries protect organizations. Email verification, MFA, background workers,
real-time updates, analytics infrastructure, and production deployment are
not implemented.

Security hardening includes bounded request bodies, authentication rate
limiting, production CORS/config validation, secure refresh-cookie settings,
JWT expiry enforcement, and security headers. Hosted deployment verification
remains required before public acceptance; scoped local verification is not
security certification.

## More Screenshots

![Order detail and timeline](docs/screenshots/order-detail.png)
![Replenishment recommendations](docs/screenshots/replenishment.png)
![Transfer detail and timeline](docs/screenshots/transfer-detail.png)
![Audit log](docs/screenshots/audit-log.png)

## Testing Story

The repository combines Go unit tests, real PostgreSQL integration tests,
concurrency tests, tenant/RBAC tests, payment and webhook tests, migration
verification, frontend lint/type/build checks, and Docker-backed demo
verification. GitHub Actions runs the normal backend and frontend checks;
extended stress matrices are not part of every CI run.

## Roadmap

CI and portfolio packaging are complete. S4 — VERIFIED (session isolation and
refresh-family protection); S5 — VERIFIED (reconciliation lifecycle);
S6 — VERIFIED (collection/batch limits). S7 — DEFERRED / NOT APPLICABLE to the
current demo because email-based membership onboarding is not implemented.
R1 — FIXED (Worker request-body buffering); R2 — FIXED (TransfersView request loop).
The first demo uses APP_ENV=production and PAYMENTS_PROVIDER=disabled.
Stripe test payments require a separate authorized security task. Public production
deployment is not active; hosted acceptance remains outstanding.

## Acknowledgements

FlowCart is independently implemented as a portfolio project using the Go,
PostgreSQL, Next.js, and Tailwind ecosystems.
