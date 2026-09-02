# FlowCart OS

FlowCart OS is a production-style platform for managing multi-warehouse
orders and fulfilment. The project is being built incrementally, with a
working frontend, Go API, and PostgreSQL infrastructure foundation.

## Current Stack

- Next.js with React and TypeScript
- Tailwind CSS
- Go with Chi and the standard `net/http` package
- PostgreSQL 18.6 for local infrastructure

## Current Status

The current milestone includes:

- A Next.js frontend at `http://localhost:3000`
- A Go HTTP API at `http://localhost:8081`
- Frontend API status reporting
- A working `GET /health` endpoint
- A Docker PostgreSQL service exposed at host port `5433`

PostgreSQL infrastructure is available, but migrations and application tables
are **not implemented yet**. Authentication, users, organizations, products,
inventory, orders, workers, Redis, and other business features are future
milestones.

## Repository Structure

```text
flowcart/
├── apps/
│   ├── api/
│   │   ├── cmd/server/
│   │   └── internal/
│   │       ├── config/
│   │       ├── database/
│   │       └── httpapi/
│   └── web/
│       └── src/app/
├── docs/
│   ├── architecture.md
│   ├── decisions.md
│   └── development.md
└── AGENTS.md
```

## Prerequisites

- Node.js and npm
- Go
- Windows PowerShell for the commands below
- Docker Desktop with the Linux engine running

## Run the Frontend

From the project root:

```powershell
cd apps\web
npm run dev
```

Open `http://localhost:3000`.

## Run PostgreSQL

The existing local PostgreSQL installation uses host port `5432`. Do not stop
or modify it. FlowCart's Docker PostgreSQL uses host port `5433`, mapped to
container port `5432`.

```text
Windows host:5433 -> PostgreSQL container:5432
```

From the project root:

```powershell
docker compose up -d postgres
docker compose ps
docker compose logs postgres
```

To stop the service:

```powershell
docker compose down
```

## Run the Backend

In a separate PowerShell window, from the project root:

```powershell
cd apps\api
$env:PORT="8081"
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable"
go run ./cmd/server
```

The backend runs at `http://localhost:8081`.

Port `8080` is intentionally unavailable on this development machine because
Oracle TNS Listener is using it. The backend port remains configurable through
the `PORT` environment variable.

## Health Endpoint

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
connectivity for each health request. No migrations or application tables
exist yet. The frontend uses `NEXT_PUBLIC_API_URL` to locate this endpoint.

## Next Milestone

The next milestone is database migrations and application tables.