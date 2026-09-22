# Bounded Concurrency Proof

The inventory package includes PostgreSQL-backed concurrent adjustment and
reservation tests. The important assertion is an invariant, not a benchmark
number: concurrent writers must never make `on_hand_quantity` negative or
reserve more than the available stock.

Relevant tests include:

- `TestInventoryRepositoryConcurrentDecrements`
- `TestInventoryConcurrentReservations`
- `TestAutomaticOrderScarceStockRace`
- `TestMultiwarehousePartialAndConcurrentFinalFulfillment`
- `TestTransferConcurrentDuplicateDispatchAndReceive`

Run them against a newly migrated disposable database:

```powershell
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart_verification_20260922_01?sslmode=disable"
cd apps\api
go test ./internal/inventory ./internal/order ./internal/fulfillment ./internal/transfer -count=1
```

This is bounded correctness evidence, not a capacity benchmark. It does not
measure sustained throughput, p95 latency, lock-wait distributions, connection
pool saturation, or behavior across multiple API processes. Those require a
separate load-test environment and production-like database resources.
