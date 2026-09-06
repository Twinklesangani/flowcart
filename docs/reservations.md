# Stock Reservations

Reservations hold part of an inventory level for a short period without
changing physical stock.

```text
on_hand_quantity = physical stock
reserved_quantity = effective active reservations
available_quantity = on_hand_quantity - reserved_quantity
```

Only reservations with `status = 'active'` and `expires_at > NOW()` consume
availability. The database stores each reservation separately so future orders,
release, expiry, auditability, and order/payment conversion can identify one
reservation at a time.

## Reserve

```text
POST /api/v1/organizations/{organizationID}/inventory/{inventoryID}/reservations
```

The request contains only a positive `quantity`. The server assigns a
15-minute UTC/database-backed TTL; clients cannot choose arbitrary expiration
times. Configurable TTL may be introduced later.

The repository begins a PostgreSQL transaction and locks the tenant-scoped
inventory row with `SELECT ... FOR UPDATE`. It verifies the product and
warehouse are active, lazily marks expired active reservations as `expired`,
calculates effective reserved quantity, and inserts a reservation only when
available stock is sufficient.

Every operation that changes physical or reserved stock uses that same
inventory-row lock. Therefore two requests competing for the final available
unit cannot both succeed.

## Release and Expiry

```text
POST /api/v1/organizations/{organizationID}/reservations/{reservationID}/release
```

Release first discovers the inventory ID, then locks the inventory row and the
reservation row in that order. An active unexpired reservation becomes
`released`; an active expired reservation becomes `expired`. Already released
or expired reservations are returned unchanged, making release idempotent.

There is no Redis worker or background expiry job in this milestone. Expiry is
lazy, and availability always applies the effective-active predicate even
before a stale row is materialized as `expired`.

## Stock Adjustments

Stock adjustment uses the same inventory lock and calculates effective reserved
quantity before updating `on_hand_quantity`. It rejects an update when the new
quantity would be negative or below effective reserved stock, returning
`409 stock_below_reserved` for the latter case.

## Tenant Isolation and RBAC

Every reservation query includes `organization_id`. Migration `000005` also
uses a composite foreign key from `(organization_id, inventory_level_id)` to
the matching inventory row. Foreign inventory or reservation IDs return `404`.

Owners, admins, and warehouse managers can reserve and release. Support and
viewer roles receive `403`. Inventory reads remain available to all members.

## Known Limitation and Future Orders

Reserve POST does not yet implement an idempotency key. A retry after an
ambiguous network failure could create duplicate reservations. Durable request
identity should be introduced with future order and payment workflows. Orders,
order items, reservation conversion, payments, Redis, background jobs, and
multi-warehouse allocation are intentionally deferred.