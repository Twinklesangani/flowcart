# S2 rate limiting

FlowCart's single API instance uses standard-library, mutex-protected token
buckets. No Redis or new dependency is required. Defaults are explicit constants
in `internal/auth/limits.go` and router construction, with no environment knobs.

| Budget | Initial burst | Continuous refill |
| --- | --- | --- |
| Login per IP | 10 | 10/minute |
| Login failures per normalized email | 5 | 5/minute |
| Registration per IP | 5 | 5/10 minutes |
| Refresh per IP | 60 | 60/minute |
| Organization creation per user | 5 | 5/10 minutes |
| Selected expensive mutations per user | 30 | 30/minute |

These are token buckets, not strict calendar-window totals. Bursts allow ordinary
short workflows; refill supports continued use without permanent lockout.
Registration is deliberately slower because it hashes passwords and creates
records. Refresh permits normal tab activity. Account failures share a budget
across IPs; normalization matches authentication's lowercase/trim rules.

Login reserves an account token before service work to bound concurrent attempts.
Invalid credentials keep the token spent. Success, database errors, and hashing
capacity rejection refund that attempt only; success does not erase other
concurrent failures. All attempted logins still consume their IP token.
S1 origin and bounded JSON validation run before login/registration admission;
malformed bodies therefore retain their S1 errors without expensive service work.

Both registration hashing and login password verification use one process-wide
two-slot semaphore. Excess work immediately receives 429 with a one-second
Retry-After, without a waiting queue. Argon2 parameters remain unchanged (about
38 MiB of Argon2 working memory across the two admitted operations, plus overhead).
Raw password helpers remain usable by tests/offline seed tooling.

The shared mutation budget covers order creation, auto-allocation, payment and
checkout creation, inventory adjustment, transfer creation, dispatch, and receive.
It runs after authentication and tenant membership checks and uses the verified
user ID across organizations. Changing IP or alternating routes does not provide
new capacity. Existing role checks and idempotency still run in their normal
locations. Reads, logout, and signed webhooks are not rate-limited by S2.

Client IP comes only from RemoteAddr, with IPv4-mapped IPv6 canonicalized.
X-Forwarded-For and X-Real-IP are ignored. Behind a proxy, callers share its
connection IP budget unless a future change explicitly adds trusted-proxy
configuration. Multiple instances would each have their own budgets.

Each of six stores holds at most 4096 entries (24576 total per router).
Keys are fixed-size SHA-256 digests rather than retained email/header strings.
Entries expire after an idle full-refill period. Traffic triggers automatic
cleanup at most once per second; after complete inactivity, stale memory is
reclaimed on the next request, and remains bounded while idle. No background
goroutine is needed. Full stores reject new identities with a short retry rather
than evict active limits. Process restarts reset budgets.

Every throttle response uses the sanitized API error envelope, status 429,
Cache-Control: no-store, and an integer Retry-After rounded up to the next token.
No counts or account identities are disclosed. The existing frontend API client
already displays this message.

Run focused tests with `go test ./internal/ratelimit ./internal/auth ./internal/httpapi -count=1`
from `apps/api`. For full regression, set DATABASE_URL to a freshly migrated
disposable database, then run `go test ./... -count=1`, `go vet ./...`, and
`go build ./...`. Never point integration tests at production data.
