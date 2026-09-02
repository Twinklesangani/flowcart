# FlowCart OS Architecture

## Current Architecture

FlowCart currently has a small browser-to-API-to-database architecture:

```text
Browser
  -> Next.js / React / TypeScript
  -> HTTP
  -> Go / Chi
  -> pgxpool
  -> PostgreSQL 18.6
```

The frontend uses Next.js, React, TypeScript, and Tailwind CSS. It calls the Go
HTTP API using the URL configured by `NEXT_PUBLIC_API_URL`.

The Go backend uses Chi, standard `net/http`, and `pgxpool` from pgx. It creates
and pings a PostgreSQL connection pool during startup. The current API surface
contains one endpoint:

- `GET /health` pings PostgreSQL and returns the API status, service name, and
  database status as JSON. It returns a non-200 response when the database is
  unavailable.

The local frontend runs on `http://localhost:3000`. The Go API runs on
`http://localhost:8081` because port `8080` is occupied by Oracle TNS Listener
on the Windows development machine. The backend port remains configurable
through `PORT`.

The existing local PostgreSQL installation uses host port `5432`. FlowCart's
Docker PostgreSQL uses host port `5433`, mapped to container port `5432`. The
connection URL comes from `DATABASE_URL`.

## Database Schema

Schema changes use explicit versioned SQL migrations through
`github.com/golang-migrate/migrate/v4`. The initial migration creates
`users`, `organizations`, and `organization_members`.

The migration tool also maintains `schema_migrations`. This is migration-tool
metadata, not an application domain table.

The schema uses PostgreSQL's `pgcrypto` extension and `gen_random_uuid()` for
UUID defaults. `organization_members.organization_id` references
`organizations.id ON DELETE CASCADE`, and `organization_members.user_id`
references `users.id ON DELETE CASCADE`.

## Future Phases

Passwords, authentication, JWT, registration, login, products, warehouses,
inventory, orders, Redis, workers, and AWS infrastructure are future phases.
No authentication or business APIs are implemented.
