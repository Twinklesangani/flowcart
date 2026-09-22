# Pricing and Order Totals

FlowCart stores money as integer minor units, never floating-point values.
For example, AUD 25.50 is stored as `2550`. PostgreSQL and Go both use signed
64-bit integers (`BIGINT` and `int64`). This avoids binary floating-point
rounding errors in prices and totals.

## Product Pricing

Products may remain legacy `NULL / NULL` after migration `000007`. New product
creation requires both `unit_price_minor` and `currency_code`. Zero is a valid
price; negative prices are rejected. Supported currencies are exactly `AUD`,
`USD`, and `INR`, normalized to uppercase. Foreign exchange is not implemented.

Pricing fields are stored directly on products because price history, regional
pricing, promotions, and customer-specific pricing are intentionally deferred.
A product price patch validates the resulting state: both fields must remain
null or both must be populated. A legacy product therefore needs both fields to
establish pricing.

Owners and admins can update prices. Warehouse managers, support, and viewers
receive `403`, following the existing product RBAC policy.

## Order Totals

Order creation derives product pricing from the tenant-scoped inventory/product
rows. Clients cannot submit authoritative prices, currency, line totals, or
subtotals.

```text
line_total_minor = unit_price_minor * quantity
subtotal_minor = sum(line_total_minor)
```

The order stores `currency_code` and `subtotal_minor`. Each order item stores
`unit_price_minor_snapshot`, `currency_code_snapshot`, and `line_total_minor`.
These are historical snapshots: changing a product price or name later does
not change an existing order.

All items in one order must use the same currency. Mixed currencies return
`409 mixed_currency_order`.

Multiplication and subtotal addition use checked `int64` arithmetic. Overflow
returns `400 order_total_overflow`.

## Transaction and Concurrency

Pricing is calculated inside the existing atomic order transaction that creates
the order, order items, and reservations. Inventory rows are locked in sorted
UUID order first. Distinct product rows are then locked in sorted UUID order and
reread before snapshots are taken.

If an order obtains the product lock before a price update, it captures the old
committed price. If the price update commits first, it captures the new price.
It never captures a half-updated product.

If price or stock validation fails, the transaction rolls back all order,
item, pricing, and reservation writes.

## Idempotency

The order request hash contains normalized client intent only:
`inventory_level_id` and `quantity`. Current product price is deliberately not
included. A retry with the same key and request after a price change returns the
original order and its original price snapshots. A changed client request still
returns `409 idempotency_key_reused`.

## Deferred Work

Payments, payment webhooks, tax, shipping fees, discounts, promotions, coupons,
foreign exchange, price history, dynamic pricing, customer-specific pricing,
subscriptions, and refunds are deferred.
