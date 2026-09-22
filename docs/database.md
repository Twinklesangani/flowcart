# M14 schema

Migration `000011_smart_allocation` adds strict `allocation_method` and
`allocation_strategy` audit metadata to orders. Existing and manual orders are
`manual` with no strategy; automatic orders use `minimize_splits_v1`.

# FlowCart OS Database

## Transfer Data

Migration 000012 adds `inventory_transfers` and `inventory_transfer_items`, then extends `inventory_movements` with transfer references and `transfer_out` / `transfer_in` movement types. Transfer foreign keys include `organization_id` to preserve tenant isolation. Destination inventory levels are created at transfer creation with the existing `(organization_id, product_id, warehouse_id)` uniqueness constraint.

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

Current migrations through `000015`:

```text
migrations/000001_core_saas_tables.up.sql
migrations/000001_core_saas_tables.down.sql
migrations/000002_authentication.up.sql
migrations/000002_authentication.down.sql
migrations/000003_products_and_warehouses.up.sql
migrations/000003_products_and_warehouses.down.sql
migrations/000004_inventory_levels.up.sql
migrations/000004_inventory_levels.down.sql
migrations/000005_inventory_reservations.up.sql
migrations/000005_inventory_reservations.down.sql
migrations/000006_orders.up.sql
migrations/000006_orders.down.sql
migrations/000007_product_pricing_and_order_totals.up.sql
migrations/000007_product_pricing_and_order_totals.down.sql
migrations/000008_payments.up.sql
migrations/000008_payments.down.sql
migrations/000009_trusted_payments_and_webhooks.up.sql
migrations/000009_trusted_payments_and_webhooks.down.sql
migrations/000010_fulfillment_and_inventory_movements.up.sql
migrations/000010_fulfillment_and_inventory_movements.down.sql
migrations/000011_smart_allocation.up.sql
migrations/000011_smart_allocation.down.sql
migrations/000012_inventory_transfers.up.sql
migrations/000012_inventory_transfers.down.sql
migrations/000013_replenishment_policies.up.sql
migrations/000013_replenishment_policies.down.sql
migrations/000014_audit_events.up.sql
migrations/000014_audit_events.down.sql
migrations/000015_auth_session_families.up.sql
migrations/000015_auth_session_families.down.sql
```

The migration tool creates `schema_migrations` to track versions. It is
migration metadata, not an application domain table.

## Application Tables

Current application tables are:

- `users`
- `organizations`
- `organization_members`
- `auth_sessions`
- `products`
- `warehouses`
- `inventory_levels`
- `inventory_reservations`
- `orders`
- `order_items`
- `payments`
- `payment_provider_events`
- `fulfillments`
- `fulfillment_items`
- `inventory_movements`
- `inventory_transfers`
- `inventory_transfer_items`
- `audit_events`

Migration highlights:

- `000006` adds tenant-safe orders, item snapshots, and reservations.
- `000007` adds integer minor-unit pricing and order totals.
- `000008` adds payment attempts and idempotency.
- `000009` adds trusted provider identity and durable webhook events.
- `000010` adds fulfillment, reservation consumption, and inventory movements.
- `000011` adds deterministic automatic allocation metadata.
- `000012` adds stock-conserving warehouse transfers.
- `000013` adds replenishment policies and low-stock recommendations.
- `000014` adds append-only audit events and timeline indexes.
- `000015` adds refresh-session families and family-level revocation.

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

`products.organization_id` and `warehouses.organization_id` reference
`organizations.id` with `ON DELETE CASCADE`. Product SKUs and warehouse codes
use case-insensitive unique expression indexes scoped to organization ID.
Repositories include organization ID in every resource query for defense in
depth. `inventory_levels` references products and warehouses with restrictive
deletes, has one unique row per organization/product/warehouse pair, and
enforces `on_hand_quantity >= 0` with a database check constraint.
`inventory_reservations` stores individually identifiable active, released, or
expired reservations. Its composite tenant foreign key references
`inventory_levels(organization_id, id)`, and positive quantities plus valid
statuses are enforced by database checks.
Orders are tenant-scoped and uniquely keyed by `(organization_id,
idempotency_key)`. Order items preserve product SKU/name snapshots and link
tenant-safely to orders and products. Order-created reservations optionally
link to an order item; Milestone 8 manual reservations remain nullable.
Products optionally store integer `unit_price_minor` and uppercase
`currency_code`; new orders store currency/subtotal and order items store
unit-price, currency, and line-total snapshots. Paired-field checks prevent
half-populated pricing states.
`payments` stores positive integer-minor-unit payment attempts with immutable
amount/currency snapshots, tenant-scoped idempotency, and pending/succeeded
partial uniqueness indexes.
Trusted payment success commits reservations without reducing physical stock.
Fulfillment consumes committed reservations and reduces
`inventory_levels.on_hand_quantity` atomically with fulfillment items and
append-only inventory movements. Consumed reservations no longer count toward
availability.

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

Email verification, password reset, MFA,
Redis, workers, and future business tables outside the current payment,
fulfillment, transfer, and audit modules are not implemented.

## Organization Authorization Data

`organization_members` is the authorization source of truth. Organization
endpoints scope queries by the route organization ID and verified membership.
The application does not trust an organization ID from the frontend by itself.

The membership roles are `owner`, `admin`, `warehouse_manager`, `support`, and
`viewer`. There are no custom roles or permission tables.
