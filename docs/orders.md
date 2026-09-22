# Orders and Order Items

Milestone 9 introduces a small tenant-owned order aggregate. An order contains
one or more order items; each item stores a product identity snapshot and owns
the reservation created for that item.

## Atomic Creation

```text
POST /api/v1/organizations/{organizationID}/orders
```

The request contains inventory level IDs and quantities. Product identity is
derived from the tenant-scoped inventory row rather than accepted separately
from the client. The API requires an `Idempotency-Key` header up to 200
characters.

Order creation runs in one PostgreSQL transaction:

1. Insert or replay the tenant-scoped idempotent order.
2. Sort all inventory IDs by UUID.
3. Lock every inventory row with `SELECT ... FOR UPDATE`.
4. Validate product/warehouse activity and effective availability.
5. Insert order items with product/SKU/name snapshots.
6. Insert linked active reservations with one server-controlled TTL.
7. Commit everything together.

Any unavailable item rolls back the order, all items, and all reservations.
Sorting locks prevents opposite multi-inventory requests from deadlocking.

## Idempotency

The normalized, UUID-sorted item list is SHA-256 hashed. The unique pair
`(organization_id, idempotency_key)` handles concurrent retries. The same key
and request hash return the same order without adding reservations. The same
key with a different hash returns `409 idempotency_key_reused`.

## Snapshots and Reservations

Order items store `product_id`, `sku_snapshot`, `product_name_snapshot`, and
quantity. Later product renames or deactivation do not alter historical order
identity. Order items do not store warehouses; reservations reference inventory
levels, leaving room for future multi-warehouse allocation.

Orders have only `pending` and `cancelled` states. `pending` does not guarantee
stock forever; its guarantee lasts only while linked reservations are effective.

Orders also snapshot integer-minor-unit pricing. The server derives one product
currency per order, calculates line totals and subtotal inside the atomic
creation transaction, and preserves those values if product pricing changes.

## Cancellation

```text
POST /api/v1/organizations/{organizationID}/orders/{orderID}/cancel
```

Cancellation is idempotent. It discovers linked inventory IDs without locks,
locks inventory rows in sorted order, locks the order, then locks linked
reservations and releases active reservations or marks expired ones. Finally it
marks the order cancelled in the same transaction.

## Tenant Isolation and RBAC

Every order, item, and reservation query includes `organization_id`. Composite
foreign keys protect order/item and reservation ownership at the database
layer. Cross-tenant order or inventory UUIDs behave as not found.

Owners, admins, and warehouse managers can create and cancel orders. All five
organization roles can read orders. Payments, shipping, allocation, returns,
refunds, pricing, tax, currency, customer accounts, and reservation conversion
are deferred to later milestones.