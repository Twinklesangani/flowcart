# FlowCart OS

FlowCart OS is a production-style platform for managing multi-warehouse
orders and fulfilment. The project is being built incrementally, beginning
with a working frontend-to-backend foundation.

## Current Stack

- Next.js with React and TypeScript
- Tailwind CSS
- Go with Chi and the standard `net/http` package

## Current Status

The current milestone includes:

- A Next.js frontend at `http://localhost:3000`
- A Go HTTP API at `http://localhost:8081`
- Frontend API status reporting
- A working `GET /health` endpoint

PostgreSQL is **not implemented yet**. Authentication, users, organizations,
products, inventory, orders, workers, Redis, and other business features are
future milestones.

## Repository Structure

```text
flowcart/
├── apps/
│   ├── api/
│   │   ├── cmd/server/
│   │   └── internal/
│   │       ├── config/
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

## Run the Frontend

From the project root:

```powershell
cd apps\web
npm run dev
```

Open `http://localhost:3000`.

## Run the Backend

In a separate PowerShell window, from the project root:

```powershell
cd apps\api
$env:PORT="8081"
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

Response:

```json
{
  "status": "ok",
  "service": "flowcart-api"
}
```

The frontend uses `NEXT_PUBLIC_API_URL` to locate this endpoint. Local
development configures it as `http://localhost:8081`.

## Next Milestone

The next milestone is PostgreSQL infrastructure. It will be added separately
and is not part of the current application.