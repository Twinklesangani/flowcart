# FlowCart OS

FlowCart OS is a production-style platform for managing multi-warehouse
orders and fulfilment. The current foundation includes a Next.js frontend, a
Go API, PostgreSQL infrastructure, initial SaaS tables, and authentication.

## Current Stack

- Next.js 16 with React and TypeScript
- Tailwind CSS
- Go with Chi, pgx/pgxpool, and standard `net/http`
- PostgreSQL 18.6 in Docker
- `github.com/golang-migrate/migrate/v4`
- Argon2id passwords and JWT access tokens

## Current Status

Implemented:

- Frontend at `http://localhost:3000`
- Go API at `http://localhost:8081`
- PostgreSQL Docker service on host port `5433`, container port `5432`
- Explicit versioned database migrations
- Register, login, refresh, logout, and protected `/me` authentication routes
- HttpOnly refresh-cookie sessions with rotation
- Health endpoint that verifies database connectivity

Authentication is identity-only. Organization authorization and RBAC are not
implemented. Email verification, password reset, MFA, OAuth/social login,
products, warehouses, inventory, orders, Redis, workers, and payments are not
implemented yet.

## Repository Structure

```text
flowcart/
├── apps/
│   ├── api/
│   │   ├── cmd/migrate/
│   │   ├── cmd/server/
│   │   ├── internal/auth/
│   │   └── migrations/
│   └── web/src/app/
├── docs/
│   ├── architecture.md
│   ├── authentication.md
│   ├── database.md
│   ├── decisions.md
│   └── development.md
└── AGENTS.md
```

## Prerequisites

- Node.js and npm
- Go
- Windows PowerShell
- Docker Desktop with its Linux engine running

The existing local PostgreSQL installation uses port `5432`. Do not stop or
modify it. FlowCart Docker PostgreSQL uses `5433:5432`.

## Start PostgreSQL

From the project root:

```powershell
docker compose up -d postgres
docker compose ps
docker compose logs postgres
```

Stop the service without deleting its volume:

```powershell
docker compose down
```

## Run Migrations

From `apps/api`:

```powershell
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable"
go run ./cmd/migrate up
```

Roll back the latest migration:

```powershell
go run ./cmd/migrate down
```

Migrations are explicit and are not run by server startup.

## Run the Backend

From `apps/api`:

```powershell
$env:PORT="8081"
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable"
$env:APP_ENV="development"
$bytes = New-Object byte[] 64
[System.Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
$env:JWT_SECRET = [Convert]::ToBase64String($bytes)
go run ./cmd/server
```

`ACCESS_TOKEN_TTL` defaults to `15m` and `REFRESH_TOKEN_TTL` defaults to `168h`.
`JWT_SECRET` is required. Generate your own random value for each local
environment and never commit it. Production deployments must provide the
secret through a secret manager or other secure environment configuration.

## Run the Frontend

From `apps/web`:

```powershell
npm run dev
```

Open `http://localhost:3000`. The frontend uses `NEXT_PUBLIC_API_URL`, keeps
access tokens in runtime memory, and uses credentials for refresh-cookie
requests.

## Health

```text
GET http://localhost:8081/health
```

```json
{
  "status": "ok",
  "service": "flowcart-api",
  "database": "ok"
}
```

## Authentication Routes

- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/logout`
- `GET /api/v1/auth/me`

See [docs/authentication.md](docs/authentication.md) for security details.
