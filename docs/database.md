# FlowCart OS Database

## Current Database

FlowCart uses PostgreSQL `18.6` in Docker for local development.

```text
Windows host port: 5433
Container port: 5432
```

The existing local PostgreSQL installation uses host port `5432`. Do not stop
or modify it. The FlowCart connection URL is:

```text
postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable
```

## Migration System

Migrations use `github.com/golang-migrate/migrate/v4` with PostgreSQL support.
The migration command is in `apps/api/cmd/migrate` and reads `DATABASE_URL`
from the environment. Migrations are explicit; the HTTP server does not run
them automatically.

Migration files are versioned SQL files with this naming pattern:

```text
<version>_<description>.up.sql
<version>_<description>.down.sql
```

The current migration is:

```text
apps/api/migrations/000001_core_saas_tables.up.sql
apps/api/migrations/000001_core_saas_tables.down.sql
```

## Windows PowerShell Workflow

Start PostgreSQL from the project root:

```powershell
docker compose up -d postgres
docker compose ps
```

Then move into the Go API directory and set the database URL:

```powershell
cd apps\api
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable"
```

Apply pending migrations:

```powershell
go run ./cmd/migrate up
```

Roll back the latest migration in a controlled local test:

```powershell
go run ./cmd/migrate down
```

Restore the schema after the rollback:

```powershell
go run ./cmd/migrate up
```

Inspect the PostgreSQL container logs when needed:

```powershell
docker compose logs postgres
```

The PostgreSQL Docker volume must not be deleted during this workflow.

## Current Tables

The migration creates these application domain tables:

- `users`
- `organizations`
- `organization_members`

The migration tool also creates `schema_migrations`. This is migration-tool
metadata, not an application domain table.

## Relationships

```text
users
   ↓
organization_members
   ↑
organizations
```

`organization_members.organization_id` references `organizations.id` with
`ON DELETE CASCADE`.

`organization_members.user_id` references `users.id` with `ON DELETE CASCADE`.

## Constraints

The `users` table has a unique constraint on `email`.

The `organizations` table has a unique constraint on `slug`.

The `organization_members` table has a unique constraint on
`(organization_id, user_id)` to prevent duplicate membership.

Membership roles are restricted to:

- `owner`
- `admin`
- `warehouse_manager`
- `support`
- `viewer`

The migration adds indexes for `organization_members.organization_id` and
`organization_members.user_id`. PostgreSQL automatically creates indexes for
the primary keys and unique constraints on `users.email`,
`organizations.slug`, and `(organization_id, user_id)`.

## UUID Defaults

The migration enables PostgreSQL's `pgcrypto` extension and uses
`gen_random_uuid()` as the default for UUID primary keys. No IDs or seed data
are inserted by the migration.

Passwords, authentication, JWT, registration, login, products, warehouses,
inventory, orders, and other business tables are not implemented yet.
