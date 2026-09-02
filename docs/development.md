# FlowCart OS Development

## Project Root

```text
C:\Users\TWINKLE SANGANI\OneDrive\Desktop\flowcart
```

Open PowerShell at the project root before running the commands below.

## Prerequisites

- Node.js and npm
- Go
- Docker Desktop with the Linux engine running

The existing local PostgreSQL installation uses port `5432`. Do not stop or
modify it.

## Run the Frontend

```powershell
cd apps\web
npm run dev
```

Frontend URL: `http://localhost:3000`

The frontend reads the backend URL from `NEXT_PUBLIC_API_URL`. Local
development uses `http://localhost:8081`.

## Run PostgreSQL

FlowCart's Docker PostgreSQL uses host port `5433` and maps it to container
port `5432`:

```text
localhost:5433 -> postgres container:5432
```

From the project root:

```powershell
docker compose up -d postgres
docker compose ps
docker compose logs postgres
```

Stop the service with:

```powershell
docker compose down
```

The Compose service is named `postgres`, uses PostgreSQL `18.6`, and stores
data in the named volume `flowcart_postgres_data`.

## Run Migrations

Migrations are explicit and are not run automatically by the HTTP server. From
`apps/api` in PowerShell:

```powershell
cd apps\api
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable"
go run ./cmd/migrate up
```

Roll back the latest migration with:

```powershell
go run ./cmd/migrate down
```

Restore the schema after a local rollback with:

```powershell
go run ./cmd/migrate up
```

The current migration files are:

```text
migrations/000001_core_saas_tables.up.sql
migrations/000001_core_saas_tables.down.sql
```

## Run the Backend

In a separate PowerShell window:

```powershell
cd apps\api
$env:PORT="8081"
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable"
go run ./cmd/server
```

Backend URL: `http://localhost:8081`

Port `8080` is currently occupied by Oracle TNS Listener on this Windows
development machine, so the local API uses port `8081`. The backend still
accepts a configurable port through `PORT`.

## Health Check

Request:

```text
GET http://localhost:8081/health
```

Response when PostgreSQL is available:

```json
{
  "status": "ok",
  "service": "flowcart-api",
  "database": "ok"
}
```

The Go API requires `DATABASE_URL` during startup and checks PostgreSQL
connectivity for each health request. It will not start if PostgreSQL cannot
be reached.
