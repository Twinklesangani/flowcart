# FlowCart OS Architecture Decisions

## Current Decisions

- Use React through Next.js and TypeScript for the frontend.
- Use Tailwind CSS for the existing frontend styling.
- Use Go with Chi, pgx/pgxpool, and standard `net/http` for the backend.
- Use PostgreSQL as the primary database in a modular monolith.
- Use explicit versioned SQL migrations with
  `github.com/golang-migrate/migrate/v4`.
- Use Argon2id with a random salt for password hashing.
- Use short-lived HS256 JWT access tokens with required `JWT_SECRET`.
- Use cryptographically random opaque refresh tokens and store only SHA-256
  hashes in `auth_sessions`.
- Rotate refresh sessions transactionally and revoke the previous session.
- Use an HttpOnly `flowcart_refresh` cookie with `Path=/`, SameSite Lax, and no
  Domain. Disable Secure for local HTTP and enable it in production.
- Keep frontend access tokens in runtime memory, never localStorage or
  sessionStorage.
- Keep authentication identity separate from future organization authorization
  and RBAC.
- Use local API port `8081` because Oracle TNS Listener occupies `8080`.
- Keep backend port configurable through `PORT`.
- Use `NEXT_PUBLIC_API_URL` for the frontend backend URL.

## Products and Warehouses

- Keep products and warehouses as separate modular-monolith domains.
- Scope every product and warehouse query by organization ID, even after
  membership middleware has verified the tenant.
- Normalize product SKUs and warehouse codes to uppercase before storage.
- Keep SKU and warehouse-code uniqueness case-insensitive and tenant-scoped.
- Use `is_active` instead of DELETE because future inventory, orders, transfers,
  and reporting may reference these operational records.
- Allow owners/admins to manage products; allow owners/admins/warehouse
  managers to manage warehouse metadata. All members can read both resources.

## Planned Architecture

Future backend features will generally follow:

```text
handler -> service -> repository -> PostgreSQL
```

Email verification, password reset, MFA, OAuth/social login, reservations, orders,
Redis, workers, and payments remain future work.

## Organization and RBAC Decisions

- Treat organization membership as the tenant authorization source of truth.
- Put the route organization ID, authenticated user ID, and verified role in
  request context only after a database membership lookup.
- Return `404 Organization not found` for non-member organization access to
  avoid leaking whether an inaccessible tenant exists.
- Allow all members to view organization details and member lists.
- Allow only owners and admins to update organizations and manage members.
- Allow only owners to assign the `owner` role; admins cannot modify owners.
- Protect last-owner role changes and removals with a PostgreSQL transaction
  and row locking.
- Keep organization authorization separate from identity authentication and
  do not put roles in the JWT.

## Inventory and Stock Safety

- Model physical stock as one inventory level per organization, product, and
  warehouse.
- Keep inventory tenant scoped and validate both related resources in the
  same organization before creation.
- Keep nonnegative stock as both a service rule and a PostgreSQL `CHECK`
  constraint.
- Change stock through a signed delta operation, not an arbitrary quantity
  patch. The repository locks the row with `SELECT ... FOR UPDATE` inside a
  short transaction before validating and updating the quantity.
- Keep reservations, orders, transfers, and allocation out of this milestone.

## Reservations and Availability

- Store reservations as individually identifiable rows rather than adding a
  denormalized counter to `inventory_levels`.
- Calculate effective reserved stock from `status = 'active'` and
  `expires_at > NOW()`.
- Use a server-controlled 15-minute TTL until configuration is needed.
- Serialize reserve, release, expiry, and stock adjustment through the same
  tenant-scoped inventory row lock.
- Keep reserve retries without an idempotency key as a known limitation until
  order and payment workflows define durable request identity.
