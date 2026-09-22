# Payment Foundation

Milestone 11 models payments as individually identifiable payment attempts.
One order may have multiple historical attempts:

```text
failed -> failed -> pending
```

A payment attempt snapshots `orders.subtotal_minor` and
`orders.currency_code` as `amount_minor` and `currency_code`. Clients never
submit authoritative payment amount, currency, or subtotal values.

## Creation and Idempotency

```text
POST /api/v1/organizations/{organizationID}/orders/{orderID}/payments
```

The request body is empty and requires an organization-scoped
`Idempotency-Key`. The server creates only `pending` attempts. Same-key/same-
order retries return the original payment, even if reservations later expire.
A reused key for another order returns `409 idempotency_key_reused`.

Payment creation serializes through sorted inventory locks, the order row, and
linked reservations. It verifies that every order item still has effective
reservation coverage (`active` and `expires_at > NOW()`). A new attempt after
reservation expiry returns `409 reservation_expired`; it never automatically
re-reserves stock.

## Attempts and Status

Statuses are `pending`, `failed`, `succeeded`, and `cancelled`. Failed attempts
may be followed by another pending attempt. A pending attempt blocks another
attempt, and a succeeded attempt blocks all later attempts. No public endpoint
can mark a payment succeeded or failed; trusted provider transitions belong to
Milestone 12.

Zero-total orders do not create payments and return `409 payment_not_required`.

## Cancellation

Order cancellation uses the existing inventory-first lock hierarchy, then locks
payment attempts. A pending payment is marked `cancelled` before reservations
are released. Failed and cancelled attempts remain unchanged. A succeeded
payment blocks ordinary cancellation with `409 payment_already_succeeded`; no
refund behavior exists yet.

## Tenant Isolation and RBAC

Payments belong to an organization and use a tenant-safe composite foreign key
to orders. Every read and write includes `organization_id`; cross-tenant IDs
return `404`.

All roles may read payment attempts. Owners, admins, and warehouse managers may
create them. Support and viewer roles receive `403` for creation.

## Future Provider Work

Milestone 12 can add provider name, provider payment ID, payment intents,
webhook signatures, webhook idempotency, and trusted succeeded/failed
transitions. Stripe, PayPal, refunds, chargebacks, authorization, capture,
PCI/card storage, Redis, workers, shipping, tax, and fulfillment are deferred.
