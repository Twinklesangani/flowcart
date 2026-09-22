# Disposable Backup and Restore Runbook

This runbook is intentionally limited to disposable PostgreSQL databases. Do not
run it against the original `flowcart` database without an approved operational
change plan.

## Create a disposable database

```powershell
$env:DATABASE_URL="postgres://flowcart:flowcart_dev@localhost:5433/flowcart_verification_20260922_01?sslmode=disable"
docker exec flowcart-postgres psql -U flowcart -d postgres -c "CREATE DATABASE flowcart_verification_20260922_01 OWNER flowcart;"
cd apps\api
go run ./cmd/migrate up
```

Confirm the target before any data operation:

```powershell
docker exec flowcart-postgres psql -U flowcart -d flowcart_verification_20260922_01 -c "SELECT current_database(), current_user;"
```

## Seed and capture a logical backup

```powershell
$env:APP_ENV="verification"
$env:FLOWCART_SEED_CONFIRM="FLOWCART_DEMO"
go run ./cmd/seed
docker exec flowcart-postgres pg_dump -U flowcart -Fc --no-owner -f /tmp/flowcart_verification_20260922_01.dump flowcart_verification_20260922_01
docker cp flowcart-postgres:/tmp/flowcart_verification_20260922_01.dump "$env:TEMP\flowcart_verification_20260922_01.dump"
```

Keep dumps outside the repository. They may contain demo credentials and
business data. Never commit them.

## Restore proof

Create a second disposable database and restore only into that target:

```powershell
docker exec flowcart-postgres psql -U flowcart -d postgres -c "CREATE DATABASE flowcart_restore_20260922_01 OWNER flowcart;"
docker exec flowcart-postgres pg_restore -U flowcart -d flowcart_restore_20260922_01 --clean --if-exists --no-owner /tmp/flowcart_verification_20260922_01.dump
```

Verify the restored data without changing it:

```powershell
docker exec flowcart-postgres psql -U flowcart -d flowcart_restore_20260922_01 -c "SELECT count(*) AS organizations FROM organizations; SELECT count(*) AS orders FROM orders; SELECT count(*) AS inventory_levels FROM inventory_levels;"
```

The proof is successful only when the counts and representative business
records match the source database. A backup file existing on disk is not proof
that it can be restored.

## Hosted equivalent

For Neon, use the provider's point-in-time recovery and logical backup options,
then restore to a separate branch or database before validation. Render and
Cloudflare do not replace PostgreSQL backups. The exact retention, recovery
point objective, and recovery time objective require paid/provider-account
configuration and are not verified in this repository.
