# FlowCart OS Architecture

## Current Architecture

```text
Browser
  -> Next.js / React / TypeScript / Tailwind
  -> HTTP with credentialed development CORS
  -> Go / Chi / net/http
  -> Authentication handler
  -> Authentication service
  -> Authentication repository
  -> pgxpool
  -> PostgreSQL 18.6
```

The browser talks to the Go API at `http://localhost:8081`. The frontend runs
at `http://localhost:3000` and gets the API base URL from
`NEXT_PUBLIC_API_URL`.

The Go API creates a PostgreSQL pool at startup and refuses to start if the
connection cannot be established. The health endpoint pings the database.

Authentication is separated into handler, service, repository, password, token,
and middleware responsibilities. Organization functionality has its own
handler, service, repository, tenant middleware, and authorization helpers.
Products and warehouses are separate tenant-owned modules following the same
handler -> service -> repository -> PostgreSQL flow. Inventory follows the same
flow and uses a repository transaction for stock adjustments.
Authentication proves identity; organization membership proves tenant access.

## Authentication Flow

Register and login hash passwords with Argon2id, create a short-lived HS256 JWT
access token, create a refresh session, and set the opaque refresh token in an
HttpOnly cookie. Refresh hashes the cookie value, rotates the session
transactionally, revokes the old session, and sends a replacement cookie.
`/me` validates the Bearer JWT through middleware and loads the current user.

## Database and Migrations

PostgreSQL Docker uses host port `5433` mapped to container port `5432`. The
existing local PostgreSQL installation on port `5432` is not modified.

Versioned SQL migrations use `github.com/golang-migrate/migrate/v4` and are run
explicitly through `apps/api/cmd/migrate`. The current schema includes
`users`, `organizations`, `organization_members`, `auth_sessions`, `products`,
`warehouses`, and `inventory_levels`.
`schema_migrations` is migration-tool metadata, not a domain table.

The authentication migration adds a case-insensitive unique index on
`LOWER(users.email)`, `users.password_hash`, and refresh-session persistence.
The `pgcrypto` extension provides `gen_random_uuid()` defaults.

## Organization Authorization

Organization routes use the authenticated user ID and route organization ID to
look up membership in PostgreSQL. The verified organization ID, user ID, and
role are stored in request context. Organization-specific repository queries
are scoped by organization ID.

Members can view their organization and member list. Owners and admins can
update organizations and manage members. Only owners can assign or manage
owners; admins cannot modify owners. Last-owner changes are protected with
transactional row locking.

## Future Phases

Email verification, password reset, MFA, reservations, orders,
Redis, workers, payments, and AWS infrastructure are not implemented.
