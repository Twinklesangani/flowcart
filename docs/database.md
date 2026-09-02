# FlowCart OS Database

## Current Database

FlowCart uses PostgreSQL `18.6` in Docker. The existing local PostgreSQL
installation uses host port `5432`; FlowCart uses host port `5433` mapped to
container port `5432`.

```text
postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable
```

## Migrations

Versioned SQL migrations use `github.com/golang-migrate/migrate/v4`. They are
explicit and are not run automatically by HTTP server startup.

From `apps/api` in Windows PowerShell:

```powershell
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable"
go run ./cmd/migrate up
go run ./cmd/migrate down
go run ./cmd/migrate up
```

Migration files use:

```text
<version>_<description>.up.sql
<version>_<description>.down.sql
```

Current migrations:

```text
migrations/000001_core_saas_tables.up.sql
migrations/000001_core_saas_tables.down.sql
migrations/000002_authentication.up.sql
migrations/000002_authentication.down.sql
```

The migration tool creates `schema_migrations` to track versions. It is
migration metadata, not an application domain table.

## Application Tables

Current application tables are:

- `users`
- `organizations`
- `organization_members`
- `auth_sessions`

Relationships:

```text
users
   ↓
organization_members
   ↑
organizations

users
   ↓
auth_sessions
```

`organization_members.organization_id` references `organizations.id` with
`ON DELETE CASCADE`. `organization_members.user_id` references `users.id` with
`ON DELETE CASCADE`. `auth_sessions.user_id` references `users.id` with
`ON DELETE CASCADE`.

## Users

`users` contains `id`, `email`, optional `first_name` and `last_name`,
`password_hash`, `created_at`, and `updated_at`. Passwords are stored as
Argon2id encoded hashes, never plaintext. The authentication migration removes
the original case-sensitive email unique index and adds one unique index on
`LOWER(email)`, preventing `User@example.com` and `user@example.com` from
becoming separate accounts. Email values are normalized in the service before
storage.

## Organizations and Membership

`organizations` contains `id`, `name`, `slug`, `created_at`, and `updated_at`.
`slug` is unique.

`organization_members` contains `id`, `organization_id`, `user_id`, `role`, and
`created_at`. `(organization_id, user_id)` is unique to prevent duplicate
membership. Roles are limited to `owner`, `admin`, `warehouse_manager`,
`support`, and `viewer`.

## Authentication Sessions

`auth_sessions` contains `id`, `user_id`, `refresh_token_hash`, `expires_at`,
`created_at`, `last_used_at`, and `revoked_at`. `refresh_token_hash` is unique,
and `user_id` has an index for session lookups. Plaintext refresh tokens are
never stored. Organization IDs are intentionally not part of this table because
authentication proves identity; authorization comes later.

## UUID Defaults

The schema enables PostgreSQL's `pgcrypto` extension and uses
`gen_random_uuid()` for UUID defaults. Migrations do not insert seed data.

## Not Implemented

Email verification, password reset, MFA, OAuth/social login, organization
authorization/RBAC, products, warehouses, inventory, orders, Redis, workers,
payments, and other business tables are not implemented.
