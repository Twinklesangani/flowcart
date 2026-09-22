# Fulfillment and Inventory Consumption

Milestone 13 represents physical fulfillment after a payment has committed stock.

## Stock Lifecycle

Payment does not reduce physical stock:

```text
on_hand = 10
committed = 2
available = 8
```

Fulfillment atomically consumes the committed reservation and deducts physical
stock:

```text
on_hand = 8
consumed = 2
available = 8
```

`consumed` reservations do not count as unavailable because their quantity has
already left `on_hand_quantity`. Active effective reservations, `payment_held`,
and `committed` reservations continue to count against availability.

## Fulfillment Model

A fulfillment belongs to one organization, order, and warehouse. Its items
consume complete committed reservations. A reservation is indivisible in this
milestone: `committed -> consumed` happens for its full quantity.

Orders can be fulfilled across warehouses with one fulfillment per warehouse:

```text
paid -> partially_fulfilled -> fulfilled
```

The final order state is derived from consumed quantities for every order item.
`paid_at` is preserved and `fulfilled_at` is set when all required quantities
are consumed.

## Atomicity and Idempotency

Fulfillment completion is one PostgreSQL transaction. It locks sorted inventory
rows, then the order, then sorted reservations. It validates payment/order
eligibility, aggregates deductions per inventory level, inserts fulfillment
items and movement rows, consumes reservations, and derives the new order state
before committing.

The request accepts only reservation IDs:

```http
POST /api/v1/organizations/{organizationID}/orders/{orderID}/fulfillments
Idempotency-Key: fulfillment-123
```

```json
{"reservation_ids":["uuid"]}
```

Reservation IDs are sorted before hashing. Replaying the same organization,
key, and reservation set returns the original fulfillment without another stock
update. Reusing a key with another request conflicts.

## Movement Ledger

`inventory_movements` is append-only audit history. Fulfillment creates one
negative `fulfillment` movement per consumed reservation. When multiple
reservations share an inventory level, the physical update is aggregated into
one inventory update while movement rows preserve reservation-level history.

Successful manual adjustments also create an `adjustment` movement in the same
transaction as the stock update. Failed mutations create no movement.

## Read Routes

- `GET /api/v1/organizations/{organizationID}/orders/{orderID}/fulfillments`
- `GET /api/v1/organizations/{organizationID}/fulfillments/{fulfillmentID}`
- `GET /api/v1/organizations/{organizationID}/inventory/{inventoryID}/movements?limit=50`

Results are tenant scoped and movement reads are bounded to a maximum of 100
rows.

## Deferred Scope

Shipping carriers, tracking, labels, returns, refunds, partial reservation
splitting, transfers, automatic warehouse allocation, queues, Redis, and
microservices are outside M13.
