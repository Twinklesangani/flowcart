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

## Planned Architecture

Future backend features will generally follow:

```text
handler -> service -> repository -> PostgreSQL
```

Email verification, password reset, MFA, OAuth/social login, organization
authorization/RBAC, products, warehouses, inventory, orders, Redis, workers,
and payments remain future work.
