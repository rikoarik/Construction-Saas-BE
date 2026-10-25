# Backend System Design — Construction SaaS

**Status:** normative · **Owner:** backend-core lane · **Stack:** Go, PostgreSQL, pgx/sqlc, Goose, S3/MinIO, Excelize, Chromium PDF

This document is the source of truth for backend implementation. The product is designed as a complete construction SaaS. Delivery may be phased, but domain boundaries, data integrity, tenant isolation, versioning, auditability, and extension points must be production-grade from the first implementation.

## 1. Backend responsibilities

The Go backend owns authentication, authorization, tenant isolation, company/project membership, project and contract data, WBS/location trees, RAB/BoQ import and versioning, schedule baselines, field-report state transitions, approval workflows, progress ledger and calculations, official report snapshots, asynchronous jobs, private-object access, audit logs, subscription entitlements, platform operations, and all business rules. Telegram Bot transport concerns are owned by the separate bot lane defined in `docs/bot/BOT.md`.

The browser and Telegram clients are untrusted presentation layers. No authoritative construction calculation, permission decision, budget mutation, approval transition, or document publication is implemented only in the frontend.

## 2. Runtime architecture

```text
Browser / Telegram Mini App
          |
          v
      Go HTTP API  ----> private object storage
          |                     ^
          v                     |
      PostgreSQL <---- Go worker / outbox
          |                     |
          +---- audit ----------+

Telegram Bot API <---- webhook adapter / notification worker
Chromium + Excelize <--- official document jobs
```

Use a modular monolith first. Modules are isolated in code and data ownership, but share one deployable API and one worker until measured scale requires extraction.

## 3. Module ownership

| Module | Owns | Key invariants |
|---|---|---|
| `identity` | users, sessions, credentials, invitations | opaque sessions, revocation, CSRF-safe mutation |
| `tenancy` | companies, memberships, company roles, entitlements | deny by default; every tenant access scoped |
| `projects` | projects, contracts, locations, WBS, project members | company/project relation cannot cross tenant |
| `budgets` | RAB imports, mappings, versions, lines, addenda | approved versions immutable |
| `planning` | baseline versions, weekly allocations, calendars | approved baseline immutable and versioned |
| `reports` | field reports, entries, evidence, workflow state | submit/approve transitions validated server-side |
| `progress` | normalized progress events, ledger, projections | final approval posts exactly once |
| `documents` | report periods, snapshots, templates, generated files | published output reproducible from immutable snapshot |
| `costs` | actual expenses, commitments, cost codes | actual cash/cost never conflated with earned value |
| `materials` | catalog, receipts, issues, stock movements | stock derived from append-only movement ledger |
| `notifications` | notification intents, preferences, in-app delivery state | at-least-once delivery, idempotent consumers |
| `integration` | provider-neutral external integration contracts | explicit adapters; no provider logic in domain modules |
| `billing` | plans, subscriptions, usage/limits | entitlements enforced independently of UI |
| `platform` | audit, support access, jobs, health, operational controls | support access explicit and audited |

Cross-module access must go through application services or stable read ports. Do not let HTTP handlers reach arbitrary tables owned by other modules.

## 4. Package convention

```text
backend/
  cmd/
    api/
    worker/
  internal/
    identity/
    tenancy/
    projects/
    budgets/
    planning/
    reports/
    progress/
    documents/
    costs/
    materials/
    notifications/
    integration/
    billing/
    platform/
    shared/
      authz/
      decimal/
      clock/
      idempotency/
      observability/
  db/
    migrations/
    queries/
    sqlc/
  api/
    openapi.yaml
  test/
    fixtures/
    integration/
```

Within a domain module prefer `domain`, `app`, `ports`, and `infra` boundaries where complexity warrants it. Avoid ceremonial layers for trivial code, but never couple domain rules directly to HTTP or a specific database driver.

## 5. Database and multi-tenancy rules

PostgreSQL is the system of record. Tenant-owned tables carry `company_id`; project-owned tables also carry `project_id`. Use composite candidate keys and foreign keys such as `(company_id, project_id)` when a child references a project. A request-provided `project_id` is never sufficient evidence of access.

Use UUID/UUIDv7-compatible identifiers where practical. Business-facing codes remain separate fields. Money, quantities, weights, percentages, and rates use PostgreSQL `NUMERIC` with explicit precision policy and decimal-safe Go values. Never use `float64` as authoritative storage or calculation input.

Approved/versioned records are immutable. Corrections are explicit events or new versions. Audit history is append-only. Hard deletion is limited to safe drafts and retention workflows; published financial/progress history is not silently deleted.

Required indexes include tenant/project/date paths, workflow status queues, import/job status, ledger effective dates, and uniqueness keys used for idempotency. Every migration has forward safety notes and a tested rollback/recovery strategy where destructive rollback is impossible.

## 6. Construction measurement model

The backend supports three normalized measurement kinds:

- `quantity`: approved delta in a physical unit, e.g. m3, m2, ton, m.
- `milestone`: completion of weighted milestone components.
- `verified_percent`: approved progress-point change for work that cannot be represented reliably as quantity.

Do not fake milestone or percentage work as quantities. Store a discriminator and enforce compatible fields with database checks.

For quantity work:

```text
item_value = contract_quantity * unit_price
item_weight_pct = item_value / eligible_approved_budget_total * 100
item_progress_pct = approved_cumulative_quantity / contract_quantity * 100
item_contribution_pct = item_weight_pct * item_progress_pct / 100
project_progress_pct = sum(item_contribution_pct)
```

For milestone/verified-percent work, the measurement adapter produces normalized item progress under an approved method. The weighted contribution formula remains the same. Over-contract progress is flagged and handled by explicit policy; it is never silently clipped.

Weekly `previous`, `current`, and `cumulative` values are computed from ledger effective dates and project timezone. Deviation is actual cumulative percentage minus planned cumulative percentage in percentage points.

## 7. Budget, addendum, and baseline versioning

RAB import creates staging data first. Users select sheet/header, map columns, preview, resolve validation errors, then create a draft budget version. Approval freezes it. Addendum creates a new version with explicit effective/cutover policy; existing published documents keep references to the versions that produced them.

Baseline plans have equivalent draft/approved/version semantics. Historical documents never resolve “latest” versions at render time; they use pinned snapshot references.

## 8. Field reporting and approval transaction

A field report follows an explicit state machine such as `draft -> submitted -> in_review -> revision_requested/rejected/approved`. Workflow configuration is versioned. Final approval must execute in one database transaction:

1. verify tenant/project permission and workflow step;
2. lock/version-check the report;
3. append approval event;
4. post normalized progress entries using unique source keys;
5. change final report state;
6. append audit event;
7. enqueue outbox events;
8. commit.

Retrying the same approval or receiving concurrent approval requests must not duplicate ledger effects.

## 9. Official document pipeline

Official XLSX/PDF generation uses an immutable report snapshot containing company/project, reporting period, budget version, baseline version, ledger cutoff, report-template version, signatory configuration, locale, tax representation, rounding policy, and a content hash.

Excel and PDF renderers read the same canonical snapshot. Generation happens asynchronously through the worker. Generated objects are private. Publication creates a durable revision; re-issuing creates a new revision instead of overwriting the old binary.

## 10. Authentication and authorization

Use Argon2id for password hashing and opaque, random session tokens stored hashed server-side. Cookies are Secure, HttpOnly, and SameSite-appropriate. Cookie-authenticated mutations require CSRF protection. Sessions have expiration, revocation, and device/audit metadata.

Authorization evaluates company membership, project membership, permission, resource ownership, subscription entitlement where applicable, and support-access policy. Platform operators do not automatically gain customer-data access.

## 11. File and object security

Uploads are private, size-limited, extension/MIME checked, sanitized, and optionally malware-scanned. The backend generates short-lived signed download URLs after authorization. Spreadsheet input is treated as untrusted data. Do not execute macros or treat formulas as authoritative business rules. Protect exports against formula injection where user-entered values enter spreadsheet cells.

## 12. Bot integration boundary

Telegram Bot behavior is specified in `docs/bot/BOT.md` and implemented as an adapter around backend application services. Backend core owns the domain operations and provider-neutral notification intents. The bot lane owns Telegram webhook verification, update parsing, callback/command routing, account-linking transport, Telegram delivery, retries, and Telegram-specific observability.

The bot must not query construction tables directly to recreate business rules. It calls application services or explicitly exposed internal ports. Telegram identity is never authorization by itself.

## 13. Cost and material domains

Actual expenses are separate from RAB earned-value calculations. Cost records may reference project, cost code/work item, vendor, document, commitment, tax, and payment state, but “cost incurred” is not inferred from physical progress.

Material inventory uses movement ledgers (`receipt`, `issue`, `return`, `adjustment`, `transfer`) and derives stock balances. Project consumption can reference field reports or work items without mutating historical progress measurements.

## 14. Subscription and platform controls

Plans/entitlements are backend-enforced. Model limits explicitly: active projects, members, storage, report generations, advanced modules, and enterprise features. Grace periods and read-only states are explicit. Never hide a disabled button while leaving the endpoint unrestricted.

## 15. API contract

REST is versioned under `/api/v1`. `backend/api/openapi.yaml` is the machine-readable contract. Handwritten frontend request/response duplicates are prohibited. Errors use stable machine codes plus structured parameters; clients localize messages.

Breaking changes require a versioned migration path. Compatible additions still require contract review because frontend mocks and generated types depend on them.

## 16. Async jobs and idempotency

Use PostgreSQL outbox initially. Workers use leases/locking, bounded retries, dead-letter visibility, idempotency keys, and deterministic job handlers. Notifications, document rendering, projection refreshes, imports, and long exports may be asynchronous. A successfully committed business transaction must not depend on an external API call succeeding in-line.

## 17. Observability

Every request/job has correlation identifiers. Structured logs exclude passwords, session tokens, raw Telegram init data, and private document contents. Measure API latency/error rate, DB pool pressure, outbox lag, import duration, export duration, queue retries, and tenant-level abusive usage. Telegram-specific metrics are defined in `docs/bot/BOT.md`.

## 18. Backend testing contract

Required layers: domain unit tests, SQL/query integration tests against real PostgreSQL, authorization negative tests, state-machine tests, idempotency/race tests, golden numerical fixtures, import fixtures, worker retry tests, object-access tests, and API contract tests.

Tests must include at least two structurally different project examples (building and road/infrastructure) to prove the domain is not accidentally hardcoded to one contractor type.

## 19. Scale evolution

Scale from measured bottlenecks. Add read models/cache before service extraction. Partition workers before creating microservices. Introduce service boundaries only when team ownership, deployment independence, data scale, or reliability requirements justify them. Kubernetes is not an architectural milestone by itself.

Enterprise capabilities such as SSO, delegated approval, audit export, advanced retention, customer-managed keys, and dedicated environments are extensions of these boundaries, not separate rewrites.
