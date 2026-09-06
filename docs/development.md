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
$env:ACCESS_TOKEN_TTL="15m"
$env:REFRESH_TOKEN_TTL="168h"
$env:APP_ENV="development"
$bytes = New-Object byte[] 64
$bytes = New-Object byte[] 64
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes)
$rng.Dispose()
$env:JWT_SECRET = [Convert]::ToBase64String($bytes)
go run ./cmd/server
```

`JWT_SECRET` is required. Generate your own random value with the PowerShell
commands above and never commit it. Production must use a secret manager or
secure environment configuration. The root `.env.example` contains only a
placeholder for `JWT_SECRET`; it must never contain a real secret.

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
POST http://localhost:8081/api/v1/organizations
GET  http://localhost:8081/api/v1/organizations
GET  http://localhost:8081/api/v1/organizations/{organizationID}
PATCH http://localhost:8081/api/v1/organizations/{organizationID}
GET  http://localhost:8081/api/v1/organizations/{organizationID}/members
POST http://localhost:8081/api/v1/organizations/{organizationID}/members
PATCH http://localhost:8081/api/v1/organizations/{organizationID}/members/{userID}
DELETE http://localhost:8081/api/v1/organizations/{organizationID}/members/{userID}
POST   http://localhost:8081/api/v1/organizations/{organizationID}/products
GET    http://localhost:8081/api/v1/organizations/{organizationID}/products
GET    http://localhost:8081/api/v1/organizations/{organizationID}/products/{productID}
PATCH  http://localhost:8081/api/v1/organizations/{organizationID}/products/{productID}
POST   http://localhost:8081/api/v1/organizations/{organizationID}/warehouses
GET    http://localhost:8081/api/v1/organizations/{organizationID}/warehouses
GET    http://localhost:8081/api/v1/organizations/{organizationID}/warehouses/{warehouseID}
PATCH  http://localhost:8081/api/v1/organizations/{organizationID}/warehouses/{warehouseID}
POST   http://localhost:8081/api/v1/organizations/{organizationID}/inventory
GET    http://localhost:8081/api/v1/organizations/{organizationID}/inventory
GET    http://localhost:8081/api/v1/organizations/{organizationID}/inventory/{inventoryID}
POST   http://localhost:8081/api/v1/organizations/{organizationID}/inventory/{inventoryID}/adjust
```

Refresh and logout requests must retain cookies. `/me` requires a Bearer access
token. The refresh token is HttpOnly and is not sent in JSON.

Organization routes require the Bearer access token. Organization-specific
routes also verify membership using the route organization ID. A non-member is
returned `404 Organization not found` so inaccessible tenant existence is not
leaked. Owner/admin mutations return `403` when the role is insufficient, and
last-owner protection returns `409`.

Inventory creation requires an active product and warehouse and is limited to
owners, admins, and warehouse managers. All members can read inventory. Stock
changes use the adjust operation with a signed `delta`; support and viewer
roles receive `403`, and an adjustment that would make stock negative returns
`409`.

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
