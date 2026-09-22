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

The development-only seed creates `FlowCart Demo Retail` with Melbourne,
Sydney, and Brisbane warehouses; 14 products; healthy, low, out-of-stock, and
unconfigured inventory; orders; payments; fulfillment; transfers; and audit
history. It is deterministic and safe to rerun for the demo organization.

Demo accounts use the password `FlowCart-Demo-Only-2026!`.

| Role              | Email                 |
| ----------------- | --------------------- |
| Owner             | owner@flowcart.demo   |
| Warehouse manager | manager@flowcart.demo |
| Viewer            | viewer@flowcart.demo  |

**DEMO ONLY - NEVER USE IN PRODUCTION.**

The seed is deterministic demo data, not a backup. It does not reproduce
manually created orders, transfers, or later workflow history. See
[docs/backup-restore.md](docs/backup-restore.md) for disposable recovery proof.

## API and deployment

The public API contract is documented in [docs/openapi.yaml](docs/openapi.yaml).
The current application is not deployed. The provisional design is Cloudflare
Workers using OpenNext for the Next.js frontend, Render Free for the Go API,
and Neon Free for PostgreSQL. Static Cloudflare Pages export is not used:
the App Router contains a dynamic order route and runtime client authentication.
Hosted browser sessions should use `app.example.com` and `api.example.com`
under the same registrable domain. The API cookie remains host-only on
`api.example.com`; exact CORS and the existing Origin checks remain enabled.
Cloudflare, Render, Neon, a custom domain, DNS, and provider pricing are not
configured or verified here. AWS is an architectural equivalent only, not a
platform used by this project.

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
JWT expiry enforcement, and security headers. Additional deployment hardening
and security verification remain planned before public hosting.

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

CI and portfolio packaging are complete. Public deployment is intentionally
blocked until S4 frontend session isolation, S5 payment reconciliation
lifecycle, S6 collection/batch limits, S7 membership/email security, and final
security verification are complete.

## Acknowledgements

FlowCart is independently implemented as a portfolio project using the Go,
PostgreSQL, Next.js, and Tailwind ecosystems.
