# Telegram Bot Task Backlog — Full Product

**Repository:** backend  
**Owner:** bot lane  
**Task prefix:** `BOT-`  
**Status:** planned unless repository evidence proves completion

Bot tasks are separate from backend-core tasks so a dedicated Claude session can work in parallel. The bot remains an adapter around backend services.

## BOT-0 — Transport foundation

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BOT-001 | Telegram adapter package and ports | backend application ports | package dependency tests |
| BOT-002 | webhook endpoint + secret verification | BOT-001 | invalid/valid secret integration tests |
| BOT-003 | update parsing and bounded input validation | BOT-002 | malformed fixture tests |
| BOT-004 | update-id deduplication | BOT-002, backend idempotency store | replay tests |
| BOT-005 | fake Telegram transport for tests | BOT-001 | deterministic provider tests |
| BOT-006 | Telegram-specific metrics/log correlation | BOT-002 | observability test/checklist |

## BOT-1 — Account linking

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BOT-010 | `/start` and deep-link parser | BOT-003 | command fixtures |
| BOT-011 | one-time account-link exchange | backend link-token service, BOT-010 | expiry/use/revoke tests |
| BOT-012 | linked/unlinked account status response | BOT-011 | permission/state tests |
| BOT-013 | unlink/revocation reaction | backend unlink event | revoked-session tests |

## BOT-2 — Mini App handoff

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BOT-020 | Mini App launch buttons/deep links | BOT-010 | callback/deeplink fixtures |
| BOT-021 | server-side Mini App initData verification adapter | backend session bootstrap | signature/freshness tests |
| BOT-022 | safe bootstrap failure responses | BOT-021 | expired/invalid auth tests |

## BOT-3 — Commands and operational callbacks

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BOT-030 | project/report quick-open commands | BOT-012, backend read ports | access tests |
| BOT-031 | approval-notification callbacks | BOT-030, backend approval ports | state/conflict tests |
| BOT-032 | callback payload signing/opaque references | BOT-031 | tamper tests |
| BOT-033 | command/help localization | BOT-030 | locale coverage tests |

## BOT-4 — Notification delivery

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BOT-040 | Telegram provider adapter for notification intents | backend `BE-083` | contract tests |
| BOT-041 | localized message renderer | BOT-040 | golden message fixtures |
| BOT-042 | Telegram delivery worker integration | backend `BE-084`, BOT-040 | worker integration tests |
| BOT-043 | rate-limit aware retries | BOT-042 | retry-after fixtures |
| BOT-044 | blocked/deactivated chat handling | BOT-042 | permanent failure tests |
| BOT-045 | delivery idempotency | BOT-042 | duplicate-job tests |
| BOT-046 | notification deep links to web/Mini App | BOT-020,BOT-041 | link fixtures |

## BOT-5 — Security and abuse controls

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BOT-050 | per-user/chat abuse/rate controls | BOT-003 | burst tests |
| BOT-051 | log redaction for Telegram secrets/auth payloads | BOT-006 | redaction tests |
| BOT-052 | callback replay/tamper protection | BOT-032 | replay fixtures |
| BOT-053 | permission recheck before privileged action | backend authz ports | revoked-access tests |

## BOT-6 — Operations and quality

| ID | Outcome | Depends on | Done evidence |
|---|---|---|---|
| BOT-060 | provider health dashboard/alerts | BOT-006,BOT-042 | runbook drill |
| BOT-061 | webhook setup/rotation runbook | BOT-002 | documented rehearsal |
| BOT-062 | dead-letter inspection/replay tooling | BOT-042 | safe replay tests |
| BOT-063 | end-to-end bot fixtures in CI | BOT-045,BOT-053 | CI passes without live Telegram |
| BOT-064 | load test for update spikes | BOT-004,BOT-050 | threshold report |

## Integration rule

A `BOT-*` task may depend on a `BE-*` capability but must not silently implement that missing backend capability inside the Telegram package. Create the backend-core dependency first, then continue the bot task against its interface.
