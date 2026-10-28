# Seed the platform dashboard

Fills a **local** database with demo companies, users, projects, plans, platform activity, and queue failures so the platform dashboard renders with data. It only writes to tables that already exist; it adds no endpoint and changes no schema.

From `backend/`, with the API's `DATABASE_URL` loaded. Embedded migrations are applied automatically, so a fresh local database works in one step:

```sh
DATABASE_URL='postgres:///construction_saas_auth_dev?host=/tmp&port=5433' go run ./cmd/seed-dashboard
```

The command refuses to run when `APP_ENV=production`, and a second run is a no-op. `-force` re-applies the dataset to restore rows that were deleted; it never duplicates them.

## What it inserts

| Table | Rows | Purpose |
| --- | --- | --- |
| `app.users` | 54 (`staffNN@seed.invalid`) | total-users KPI and member counts |
| `app.companies` | 14 (12 active, 2 suspended) | company KPI, growth chart, status donut, recent table |
| `app.company_memberships` | 53 | member counts, one active owner per company |
| `app.projects` | 28 (14 active) | active-project KPI and the projects column |
| `app.plans` | 4 (Trial, Starter, Business, Enterprise) | plan labels in the recent-companies table |
| `app.subscriptions` | 14 (13 current, 1 canceled) | distribution widget and per-company plan |
| `app.outbox_events` | 8 (4 failed, 2 retry-pending, 2 delivered) | failed-process KPI and attention table |
| `app.audit_logs` | 5 | suspend/reactivate/subscription entries in the activity feed |

The plan catalog is demo data, not a product decision: production starts with an empty catalog and the distribution widget shows its empty state until plans are assigned.

## What it deliberately does not do

- **No login.** Demo accounts use the reserved `@seed.invalid` domain and an Argon2id-shaped hash with no known preimage. They can never authenticate, so the dashboard still requires a real superadmin account (see `cmd/create-superadmin`).
- **No worker heartbeat.** Integration health only reports the worker as `healthy` while a worker is actually polling, so a seeded heartbeat would be a lie within 30 seconds. Run `cmd/worker` to see that row turn healthy; the seeded email dead letter makes the email row read `degraded` on purpose.
- **No storage data.** No authoritative storage accounting exists, so the storage panel stays unavailable regardless of seeded rows.
- **No subscription management.** The seed assigns plans directly; the SA-006 endpoints (plan definitions, entitlement overrides, usage limits) are not implemented.

## Removing it

Every demo row carries a fixed identifier, so the dataset can be removed by prefix in one transaction:

```sql
BEGIN;
DELETE FROM app.subscriptions WHERE id::text LIKE 'e0000000-%';
DELETE FROM app.plans WHERE id::text LIKE 'f0000000-%';
DELETE FROM app.company_memberships WHERE company_id::text LIKE 'c0000000-%';
DELETE FROM app.projects WHERE company_id::text LIKE 'c0000000-%';
DELETE FROM app.companies WHERE id::text LIKE 'c0000000-%';
DELETE FROM app.users WHERE id::text LIKE 'a0000000-%';
DELETE FROM app.outbox_events WHERE idempotency_key LIKE 'seed-dashboard-%';
COMMIT;
```

`app.audit_logs` is append-only by design, so its five demo rows stay in the database.
