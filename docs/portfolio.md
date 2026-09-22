# FlowCart OS Portfolio Description

## Project

FlowCart OS is a multi-tenant, multi-warehouse order and fulfillment platform
with a Go/PostgreSQL backend and a Next.js operations frontend.

## Problem

Warehouse operations connect several stateful workflows: inventory must be
reserved without overselling, orders may span warehouses, payments can be
retried or arrive through webhooks, and fulfillment must consume the right
stock. The project explores how to make those workflows correct and visible to
operators.

## What I Built

- Tenant-scoped organizations and role-based access control.
- Products, warehouses, inventory, reservations, pricing snapshots, orders,
  payments, fulfillment, transfers, replenishment, and audit timelines.
- A typed operations UI for dashboards, orders, inventory, transfers, and audit
  history.
- A deterministic Docker demo dataset for repeatable walkthroughs.

## Architecture

The browser uses Next.js and TypeScript to call a Go/Chi API. The API follows a
handler -> service -> repository -> PostgreSQL structure. Stripe is an optional
provider and verified webhook integration; Redis, Kafka, and microservices are
not part of the current system.

## Hard Engineering Problems

The most important work was not adding screens. It was preserving invariants:
PostgreSQL transactions and row locks protect stock operations, idempotency
keys make retries safe, tenant IDs scope every organization query, provider
events are persisted before reconciliation, and transfers conserve stock from
dispatch through receipt. Append-only audit events make those decisions
inspectable afterwards.

## What I Learned

I learned to model business workflows as explicit state transitions, keep
business rules out of HTTP handlers, use the database as a concurrency boundary,
and verify integration behavior against real PostgreSQL rather than only mocks.
I also learned that a credible portfolio project needs a repeatable demo and
clear operational documentation alongside the code.

## Technology

Go, Chi, pgx, PostgreSQL, Next.js 16, React, TypeScript, Tailwind CSS,
Argon2id, JWT access tokens, HttpOnly refresh cookies, and an optional Stripe
adapter.

## Demo Highlights

The seeded demo shows low-stock recommendations, automatic multi-warehouse
allocation, paid and fulfilled orders, pending/in-transit/completed transfers,
and organization audit history. Screenshots are available in
[docs/screenshots](screenshots/).

## Resume Bullets

- Built a multi-tenant order and fulfillment platform with Go, PostgreSQL,
  Next.js, TypeScript, and organization-level RBAC.
- Implemented transaction and row-locking workflows for reservations,
  multi-warehouse allocation, fulfillment, and stock-conserving transfers.
- Designed idempotent order, payment, webhook, and transfer operations with
  trusted provider-event reconciliation.
- Delivered a typed operations frontend with dashboards, replenishment,
  timelines, audit history, and Docker-backed PostgreSQL verification.
