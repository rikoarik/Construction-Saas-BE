# Backend Task Backlog — Full Product

**Owner:** backend lane · **Status:** planned unless repository evidence says otherwise

This backlog covers the complete product architecture. Delivery is phased only to control dependencies and parallel execution.

Status: `planned -> contract_ready -> in_progress -> in_review -> verified -> released` (use `blocked` when required). Every task must link implementation evidence and tests before `verified`.

## B0 — Architecture, contracts, and platform foundation

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-001 | Go monorepo/API/worker scaffold with CI | — | build, lint, test pipelines |
| BE-002 | PostgreSQL connection, sqlc, pgx, Goose baseline | BE-001 | migration/query test |
| BE-003 | Decimal/precision package and numeric ADR | BE-001 | golden numeric tests |
| BE-004 | Request IDs, structured logging, error envelope | BE-001 | middleware tests |
| BE-005 | OpenAPI validation/generation pipeline | BE-001 | contract drift CI |
| BE-006 | PostgreSQL outbox + worker lease framework | BE-002 | retry/dedupe tests |
| BE-007 | Private object-storage adapter | BE-001 | signed access integration test |
| BE-008 | audit event infrastructure | BE-002 | append-only audit tests |

## B1 — Identity, tenancy, permissions

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-010 | user/password/session schema | BE-002 | migrations + query tests |
| BE-011 | Argon2id login/session/revocation | BE-010 | auth integration tests |
| BE-012 | CSRF/rate limit/security middleware | BE-011 | negative tests |
| BE-013 | company + membership + invitation | BE-010 | scoped API tests |
| BE-014 | project membership/permission engine | BE-013 | default-deny matrix tests |
| BE-015 | cross-tenant negative suite harness | BE-014 | IDOR tests across resources |
| BE-016 | support-access elevation + audit | BE-008,BE-014 | explicit grant/revoke tests |

## B2 — Projects, contracts, WBS, locations

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-020 | project/contract domain and CRUD | BE-014 | scoped API + constraints |
| BE-021 | hierarchical WBS | BE-020 | building/road fixtures |
| BE-022 | typed project locations/segments/zones | BE-020 | hierarchy tests |
| BE-023 | project team assignment and roles | BE-014,BE-020 | permission tests |
| BE-024 | project/company settings + timezone/calendar | BE-020 | boundary/date tests |
| BE-025 | project templates without type hardcoding | BE-021,BE-022 | template clone tests |

## B3 — RAB / BoQ and addenda

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-030 | budget version/line/import staging schema | BE-020,BE-003 | constraints + migrations |
| BE-031 | safe XLSX upload/parser | BE-007,BE-030 | malicious/large fixture tests |
| BE-032 | sheet/header detection and column mapping | BE-031 | mapping fixtures |
| BE-033 | locale-aware numeric parser and row validation | BE-003,BE-032 | golden import tests |
| BE-034 | subtotal/header/leaf classification | BE-033 | no double-count tests |
| BE-035 | draft budget creation | BE-034 | API integration tests |
| BE-036 | immutable budget approval | BE-035,BE-008 | state/audit tests |
| BE-037 | addendum/version cutover policy | BE-036 | historical-version tests |
| BE-038 | saved company import mappings | BE-032,BE-013 | tenant-scoped CRUD tests |

## B4 — Planning and baseline

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-040 | baseline/version schema | BE-036 | migrations |
| BE-041 | weekly allocation service | BE-040,BE-003 | total/date validation |
| BE-042 | baseline approval/immutability | BE-041 | version tests |
| BE-043 | calendar/week/timezone normalization | BE-024,BE-041 | year-boundary tests |
| BE-044 | rebaseline/version comparison | BE-042 | historical comparison tests |

## B5 — Field reports and approvals

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-050 | report/entry/evidence schema | BE-021,BE-022 | composite FK tests |
| BE-051 | quantity/milestone/percent typed measurement model | BE-003,BE-050 | DB + domain invariants |
| BE-052 | report draft/update/submit | BE-050 | state tests |
| BE-053 | private evidence upload/access | BE-007,BE-052 | object authorization tests |
| BE-054 | versioned approval workflow | BE-014,BE-052 | role/step tests |
| BE-055 | revision/reject/resubmit flow | BE-054 | state-machine tests |
| BE-056 | atomic final approval + progress posting | BE-006,BE-051,BE-054 | race/idempotency tests |
| BE-057 | configurable typed report fields | BE-052 | schema/version tests |

## B6 — Progress and project controls

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-060 | append-only normalized progress ledger | BE-056 | unique source tests |
| BE-061 | item weighted progress engine | BE-060,BE-036 | golden numeric fixtures |
| BE-062 | previous/current/cumulative period engine | BE-061,BE-043 | period fixtures |
| BE-063 | planned-vs-actual S-curve | BE-061,BE-042 | reconciliation tests |
| BE-064 | deviation/exception calculations | BE-063 | pp/overrun tests |
| BE-065 | dashboard/read-model endpoints | BE-061,BE-063 | performance/integration tests |
| BE-066 | correction/reversal events | BE-060 | audit/reconciliation tests |

## B7 — Official documents and template engine

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-070 | canonical immutable report snapshot | BE-062,BE-063,BE-008 | hash/version tests |
| BE-071 | weekly physical/value XLSX renderer | BE-070 | golden workbook |
| BE-072 | time-schedule/S-curve XLSX renderer | BE-070 | golden workbook |
| BE-073 | PDF renderer | BE-070 | visual golden |
| BE-074 | document publication/revision/download | BE-006,BE-007,BE-070 | immutable revision tests |
| BE-075 | company branding/signatory template config | BE-074 | schema/version compatibility |
| BE-076 | advanced report-template schema | BE-075 | validated template test suite |

## B8 — Notification foundation (provider-neutral)

Telegram-specific tasks are intentionally excluded from this file. They live in `docs/delivery/BOT-TASKS.md`.

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-080 | notification intent/event model | BE-006,BE-013 | event schema + idempotency tests |
| BE-081 | user notification preferences and routing policy | BE-013,BE-080 | preference/tenant-scope tests |
| BE-082 | in-app notification feed | BE-006,BE-081 | read/unread + pagination tests |
| BE-083 | provider adapter port for external channels | BE-080 | interface/contract tests |
| BE-084 | retry/dead-letter state for external deliveries | BE-006,BE-083 | retry/idempotency tests |

## B9 — Actual costs, commitments, materials

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-090 | cost code/vendor/expense model | BE-020,BE-003 | domain/migration tests |
| BE-091 | expense/commitment/payment lifecycle | BE-090 | state + audit tests |
| BE-092 | cost vs budget/progress reporting endpoints | BE-061,BE-091 | reconciliation fixtures |
| BE-093 | material catalog/unit model | BE-020 | scoped CRUD tests |
| BE-094 | material movement ledger | BE-093 | balance invariant tests |
| BE-095 | project stock and consumption views | BE-094 | movement reconciliation |
| BE-096 | optional report/work-item material references | BE-052,BE-094 | cross-domain integrity tests |

## B10 — SaaS commercial/platform

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-100 | plan/entitlement model | BE-013 | entitlement tests |
| BE-101 | subscription lifecycle abstraction | BE-100 | state tests |
| BE-102 | usage/storage/project/member metering | BE-100 | deterministic counters |
| BE-103 | backend feature/limit enforcement | BE-100 | bypass-negative tests |
| BE-104 | billing-provider adapter boundary | BE-101 | fake-provider integration |
| BE-105 | platform tenant administration | BE-016,BE-100 | audited admin tests |

## B11 — Scale, enterprise, operations

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BE-110 | backup/PITR/restore procedures | BE-002,BE-007 | recorded restore drill |
| BE-111 | health/readiness/metrics | BE-001,BE-006 | observability checks |
| BE-112 | per-tenant quotas/fair worker scheduling | BE-102,BE-006 | load/fairness tests |
| BE-113 | read-model/cache strategy from measured bottleneck | BE-065 | before/after benchmark |
| BE-114 | SSO/SAML/OIDC enterprise boundary | BE-011 | provider integration tests |
| BE-115 | audit export/retention controls | BE-008 | retention/export tests |
| BE-116 | delegated approval/absence rules | BE-054 | permission/audit tests |
| BE-117 | data export/deletion lifecycle | BE-013,BE-115 | compliance workflow tests |
| BE-118 | incident/support access runbook hooks | BE-016,BE-111 | operational exercise |
