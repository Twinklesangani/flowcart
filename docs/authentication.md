# FlowCart OS Authentication

## Architecture

Authentication proves the identity of a user. Organization authorization and
RBAC will be added later and are deliberately separate.

```text
HTTP handler -> auth service -> auth repository -> PostgreSQL
```

The handler handles HTTP JSON and status codes. The service normalizes emails,
validates credentials, hashes passwords, issues tokens, and rotates sessions.
The repository contains SQL operations for `users` and `auth_sessions`.

## Passwords

Passwords are hashed with Argon2id using:

- Memory: 19 MiB (`19456` KiB)
- Iterations: `2`
- Parallelism: `1`
- Salt: 16 cryptographically random bytes
- Hash: 32 bytes

Hashes use an encoded format containing the algorithm, version, parameters,
salt, and hash. Verification parses those parameters, recomputes the hash, and
uses constant-time comparison. Plaintext passwords and password hashes are
never returned by the API or logged.

## Access Tokens

Access tokens are short-lived JWTs signed with HS256 using the required
`JWT_SECRET`. Claims contain the user UUID as `sub`, issued and expiry times,
a unique JWT ID, and `type=access`. The parser requires HS256, validates the
signature and expiry, and rejects other token types or algorithms.

The frontend keeps the access token in React runtime memory only. It is never
stored in `localStorage` or `sessionStorage`.

## Refresh Tokens

Refresh tokens are generated with 32 random bytes and base64url encoded. This
provides at least 256 bits of randomness. The plaintext token is sent only in
the `flowcart_refresh` HttpOnly cookie. PostgreSQL stores only its SHA-256
hash in `auth_sessions.refresh_token_hash`.

`POST /api/v1/auth/refresh` verifies the cookie-backed session, rejects missing,
revoked, or expired sessions, revokes the old session, creates a replacement,
and sends a new cookie. Rotation is performed in one database transaction.
The old refresh token cannot be reused.

## Cookies and CORS

The refresh cookie uses:

- Name: `flowcart_refresh`
- `HttpOnly=true`
- `Path=/`
- `SameSite=Lax`
- `Secure=false` for local HTTP development
- `Secure=true` when `APP_ENV=production`
- No Domain attribute
- Expiration aligned with the refresh session

The API allows the known frontend origins on ports 3000 and 3001, allows
credentials, and never uses wildcard origins with credentials.

## Routes

- `POST /api/v1/auth/register`: creates a user, returns safe user data and an
  access token, and sets a refresh cookie. It does not create an organization.
- `POST /api/v1/auth/login`: normalizes the email, verifies Argon2id, and
  returns a generic invalid-credentials error on failure.
- `POST /api/v1/auth/refresh`: rotates the refresh session and returns a new
  access token without exposing the refresh token in JSON.
- `POST /api/v1/auth/logout`: revokes the cookie session when present, clears
  the cookie, and returns `204`; it is idempotent.
- `GET /api/v1/auth/me`: requires `Authorization: Bearer <access_token>` and
  returns safe current-user data.

## Configuration

```powershell
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart?sslmode=disable"
$env:JWT_SECRET="local-development-secret-change-me"
$env:ACCESS_TOKEN_TTL="15m"
$env:REFRESH_TOKEN_TTL="168h"
$env:APP_ENV="development"
```

`JWT_SECRET` is required and must be replaced with a strong secret outside
local development. No dotenv package is used.

## Frontend Session Flow

On page load the frontend calls refresh with `credentials: "include"`, then
calls `/me` with the returned in-memory access token. Login and registration
also keep the returned access token in memory. Logout calls the backend and
clears local runtime state.

## Not Implemented

Email verification, password reset, MFA, OAuth/social login, organization
creation, invitations, organization authorization/RBAC, and admin workflows
are not implemented.
