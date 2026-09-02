# FlowCart OS Development

## Project Root

```text
C:\Users\TWINKLE SANGANI\OneDrive\Desktop\flowcart
```

Prerequisites are Node.js/npm, Go, Windows PowerShell, and Docker Desktop with
the Linux engine running. The existing local PostgreSQL uses port `5432`; do
not stop or modify it. FlowCart Docker PostgreSQL uses host `5433` and
container `5432`.

## Start PostgreSQL

From the project root:

```powershell
docker compose up -d postgres
docker compose ps
docker compose logs postgres
```

Stop it with `docker compose down`. Do not delete the Docker volume.

## Migrations

From `apps/api`:

```powershell
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable"
go run ./cmd/migrate up
go run ./cmd/migrate down
go run ./cmd/migrate up
```

Migrations are explicit and are not run by HTTP server startup.

## Start the Backend

```powershell
cd apps\api
$env:PORT="8081"
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable"
$env:JWT_SECRET="local-development-secret-change-me"
$env:ACCESS_TOKEN_TTL="15m"
$env:REFRESH_TOKEN_TTL="168h"
$env:APP_ENV="development"
go run ./cmd/server
```

Backend URL: `http://localhost:8081`. Port `8080` is occupied by Oracle TNS
Listener on this Windows development machine.

## Start the Frontend

In another PowerShell window:

```powershell
cd apps\web
npm run dev
```

Frontend URL: `http://localhost:3000`. The frontend uses
`NEXT_PUBLIC_API_URL=http://localhost:8081` and does not store tokens in browser
storage.

## Authentication Testing

Routes:

```text
POST http://localhost:8081/api/v1/auth/register
POST http://localhost:8081/api/v1/auth/login
POST http://localhost:8081/api/v1/auth/refresh
POST http://localhost:8081/api/v1/auth/logout
GET  http://localhost:8081/api/v1/auth/me
```

Refresh and logout requests must retain cookies. `/me` requires a Bearer access
token. The refresh token is HttpOnly and is not sent in JSON.

## Health Check

```text
GET http://localhost:8081/health
```

Expected response while PostgreSQL is available:

```json
{
  "status": "ok",
  "service": "flowcart-api",
  "database": "ok"
}
```
