# Inventory and Stock Safety

An inventory level answers: how many units of one product are physically on
hand in one warehouse? It connects a tenant-owned product and warehouse, so a
row belongs to exactly one organization, product, and warehouse combination.
The unique constraint prevents duplicate levels for that combination.

## Creation and Tenant Isolation

`POST /api/v1/organizations/{organizationID}/inventory` accepts `product_id`,
`warehouse_id`, and an optional nonnegative `on_hand_quantity`. The verified
organization membership context supplies the tenant; `organization_id` is not
trusted from request JSON. The repository checks both foreign resources using
the tenant ID, and new inventory requires both resources to be active. An
inactive product or warehouse returns `409`; an unknown or foreign resource is
treated as `404`.

Inventory list responses join products and warehouses in one tenant-scoped
query, returning SKU, product name, warehouse code, and warehouse name without
N+1 requests. Single-row reads also include the organization predicate, so a
foreign inventory UUID is not exposed.

## Nonnegative Stock

The service rejects negative initial quantities and the database also enforces
`CHECK (on_hand_quantity >= 0)`. The database constraint is defense in depth:
it protects the invariant even if another code path writes directly to the
table.

Stock changes are business operations, not ordinary metadata edits. The
adjust endpoint is:

`POST /api/v1/organizations/{organizationID}/inventory/{inventoryID}/adjust`

with a signed `delta`. Receiving stock uses a positive delta; damage or a
write-off uses a negative delta. A direct arbitrary `PATCH` is avoided because
the server must evaluate the change against the current quantity.

## Concurrency

The repository starts a PostgreSQL transaction, locks the tenant-scoped row
with `SELECT ... FOR UPDATE`, reads the current quantity, and calculates the
result. A negative result returns `ErrInsufficientStock` and rolls back.
Otherwise it updates the row and commits. The lock makes concurrent decrements
serialize: after one request changes 5 to 1, a second request attempting -4
reads 1 and is rejected. Concurrent positive adjustments are also serialized,
so their increments are not lost.

## RBAC and Scope

Owners, admins, and warehouse managers can create levels and adjust stock.
Owners, admins, warehouse managers, support, and viewers can list and read
inventory. Support and viewer writes return `403`.

Reservations, reserved quantity, available-to-promise, orders, allocation,
transfers, alerts, and audit events are intentionally not implemented yet.