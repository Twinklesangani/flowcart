# Allocation

FlowCart supports two order allocation modes. Manual orders keep the existing
explicit inventory-level request. Automatic orders accept product quantities
and use `minimize_splits_v1` to select inventory for the organization.

Automatic allocation is a deterministic heuristic, not a globally optimal
solver. It first selects one warehouse when that warehouse can satisfy every
requested product. The tie-break is aggregate post-allocation headroom,
warehouse code, then warehouse UUID. Otherwise it greedily selects warehouses
by complete product lines, units satisfied, fewer product lines touched,
post-allocation headroom, warehouse code, and warehouse UUID.

Candidate inventory is tenant-scoped and is planned from a locked snapshot.
Inventory rows are discovered, sorted by UUID, locked, expired reservations are
evaluated, and effective availability is recalculated before the allocator
runs. Effective unavailable quantity includes non-expired active reservations,
`payment_held`, and `committed`; consumed reservations are excluded because
physical stock has already been deducted.

Automatic orders are all-or-nothing. A product can be allocated from multiple
warehouses, but it remains one order item with multiple order-linked
reservations. Insufficient network stock rolls back the order, items, and
reservations together. Inactive products and warehouses are never selected.

The `Idempotency-Key` is organization-scoped and hashes only sorted product IDs
and requested quantities. A fast lookup and a second transactional lookup
protect concurrent first requests. Replays return the original order and
reservations even after stock changes; allocation is not run again.

Pricing is locked separately after candidate inventory locks. The order stores
unit price, currency, and line-total snapshots and remains single-currency.
Allocation metadata is stored on `orders` with a strict manual/automatic
constraint. Geographic optimization, shipping cost, carrier rates, and ETA
optimization remain deferred.