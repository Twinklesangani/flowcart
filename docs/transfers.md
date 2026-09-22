# Warehouse Transfers

M15 introduces organization-scoped warehouse transfers with a physical in-transit state.

## Lifecycle

Transfers move through:

```text
pending -> in_transit -> completed
pending -> cancelled
```

Pending cancellation does not change inventory. In-transit transfers cannot be cancelled because the stock has already left the source warehouse.

## Stock Semantics

Dispatch deducts source `on_hand_quantity` and records a negative `transfer_out` movement. The destination is unchanged until receipt. While a transfer is in transit, its item quantity is included in physical stock accounting as in-transit stock.

Receipt increases destination `on_hand_quantity` and records a positive `transfer_in` movement. Dispatch and receipt are atomic across all items, so a multi-product transfer cannot partially apply.

## Safety

Transferable stock is `on_hand_quantity` minus active effective reservations, payment-held reservations, and committed reservations. Consumed reservations do not reduce transferable stock. Inventory rows are locked in global UUID order, matching allocation, fulfillment, reservation, adjustment, and opposite-direction transfer operations.

Destination inventory levels are resolved during creation with a unique constraint and `ON CONFLICT`, so concurrent transfers share one zero-stock destination row.

## Idempotency and Access

Creation, dispatch, and receipt require idempotency keys. Replays do not create duplicate stock changes or movement rows. Owners, admins, and warehouse managers may mutate transfers; organization members may read them.

Request hashes and internal idempotency metadata are not returned by read endpoints.

## Deferred Scope

M15 does not include partial receipt, damaged or missing inventory, return transfers, tracking, automatic replenishment, low-stock alerts, reorder points, or target stock levels. Those remain future work.
