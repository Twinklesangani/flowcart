# Five-Minute Interview Demo

## Sequence

1. **Problem, 20-30 seconds:** FlowCart coordinates inventory, orders, payments,
   fulfillment, and warehouse movement while protecting tenant and stock
   correctness.
2. **Login:** Use the seeded owner account and show the `FlowCart Demo Retail`
   organization context.
3. **Dashboard:** Point out mixed orders, low stock, out-of-stock inventory, and
   warehouse coverage.
4. **Replenishment:** Open Inventory, select a low-stock recommendation, and
   show the donor warehouse and quantity.
5. **Transfer:** Open an in-transit transfer and show source, destination,
   item quantity, action state, and transfer timeline.
6. **Order:** Open the fulfilled order and show allocation, payment, fulfillment,
   and order timeline data.
7. **Audit:** Filter the audit log to show order and transfer events.
8. **Correctness:** Explain row locking, reservation states, and idempotency
   keys.
9. **Architecture:** Walk from Browser -> Next.js -> Go/Chi -> services ->
   repositories -> PostgreSQL, with Stripe as a separate provider integration.
10. **Close:** Mention the modular-monolith tradeoff and what would change at
    larger scale.

Keep the walkthrough focused on decisions and invariants rather than clicking
through every screen.

## Likely Questions

### Why Go?

Go gives the API a small runtime surface, explicit concurrency primitives, and
straightforward HTTP/service/repository boundaries. It is a good fit for a
transaction-heavy modular monolith without introducing framework complexity.

### Why PostgreSQL?

The hardest guarantees are relational and transactional: tenant-scoped foreign
keys, unique idempotency identities, row locks, state checks, and atomic
inventory changes. PostgreSQL is the consistency boundary rather than just a
persistence detail.

### Why a modular monolith?

The workflows share transactions and invariants. Keeping them in one deployable
system makes correctness easier to reason about while preserving domain
boundaries that could be separated later if a real scaling need appears.

### How do you prevent overselling?

Inventory and related reservation rows are locked in deterministic order inside
transactions. Availability is calculated while those locks are held, and the
database also enforces non-negative stock and tenant-safe relationships.

### Why not Redis?

The current correctness requirements belong in PostgreSQL transactions. Redis
could help with caching or rate limiting at a larger deployment, but adding it
now would create another consistency surface without a demonstrated need.

### How does idempotency work?

Requests carry scoped idempotency keys and request hashes. Replays return the
original logical operation when the payload matches; reusing a key for a
different payload is rejected.

### How is multi-tenancy enforced?

The organization membership middleware resolves the route organization and
role, then places the verified tenant context in the request. Repositories
include organization IDs in resource queries and composite relationships.

### How do Stripe webhooks become trusted?

The webhook signature is verified before a provider event is persisted. The
payment/provider identity, amount, currency, status, and livemode are checked
again during transactional reconciliation before payment and reservation state
changes.

### How do transfers conserve stock?

Dispatch deducts source stock and records a `transfer_out` movement. Receipt
adds destination stock and records `transfer_in`. The transfer item is the
shared identity, so the movement history and aggregate remain connected.

### What would you change at larger scale?

I would first measure real bottlenecks, then consider read models for dashboard
queries, background reconciliation workers, managed observability, and carefully
chosen service boundaries. I would not split the system or add a queue before
those pressures were real.
