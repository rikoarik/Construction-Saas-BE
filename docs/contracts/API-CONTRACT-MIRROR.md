# API Contract Mirror — Frontend Consumer Copy

> **GENERATED/READ ONLY.** Canonical source: backend repository `docs/API-CONTRACT.md`. Do not edit this mirror directly.


**Status:** Proposed; future executable source: `api/openapi.yaml` + generated API client.  
**Owner:** Go Backend · **Root:** `/api/v1` · **Transport:** JSON over HTTPS, multipart/form-data only when approved.  
**See:** [System Design](../architecture/SYSTEM-DESIGN.md), [State Machines](STATE-MACHINES.md)

## 1. Global rules

- Authentication: secure opaque session cookie for web; Telegram Mini App obtains backend session only after signed `initData` verification. CSRF defense for cookie-authenticated mutations.
- Authorization: company membership + resource-specific project permission checked on **every** request. `company_id` supplied by client is only a selector.
- `Idempotency-Key` required for mutating approval/publish/import-finalize endpoints, recorded scoped by actor/tenant/action.
- Use `application/json`, ISO8601 timestamps with offset/UTC; date-only for period dates in project timezone; decimal numeric fields transported as **strings**, e.g. `"19.04"`.
- Stable errors: `400 VALIDATION_ERROR`, `401 UNAUTHENTICATED`, `403 FORBIDDEN`, `404 NOT_FOUND` (no existence leaks), `409 CONFLICT`, `422 BUSINESS_RULE_VIOLATION`, `429 RATE_LIMITED`, `500 INTERNAL_ERROR`.
- Paginated collection: `items`, `next_cursor`; all filters validated; no unbounded page size.
- `ETag`/entity `version` or equivalent for optimistic concurrency where reports can be revised.

## 2. Resources/endpoints P0

| Method | Endpoint | Permission | Core behavior |
|---|---|---|---|
| POST | `/auth/login` | public | Establish session, rate limited |
| POST | `/auth/logout` | authenticated | Revoke current session |
| POST | `/companies` | authenticated owner candidate | Create tenant + owner membership |
| POST | `/companies/{companyId}/invitations` | members.invite | Scoped invitation |
| GET | `/companies/{companyId}/members` | members.read | List permitted members |
| POST | `/companies/{companyId}/projects` | project.create | Create project |
| GET | `/companies/{companyId}/projects` | project.read | Scoped list |
| GET | `/projects/{projectId}` | project.read | Project details |
| POST | `/projects/{projectId}/work-items` | wbs.manage | Create WBS nodes |
| POST | `/projects/{projectId}/budget-imports` | budget.import | Staging XLSX; private upload |
| PUT | `/budget-imports/{importId}/mapping` | budget.import | Column mapping + parse preview |
| POST | `/budget-imports/{importId}/finalize` | budget.import | Create draft budget version |
| POST | `/budget-versions/{versionId}/approve` | budget.approve | Immutable approved budget |
| POST | `/projects/{projectId}/baselines` | baseline.create | Draft weekly plan |
| POST | `/plan-baselines/{baselineId}/approve` | baseline.approve | Freeze target schedule |
| POST | `/projects/{projectId}/reports` | report.create | Draft daily report |
| PATCH | `/reports/{reportId}` | report.edit_own | Edit draft or returned revision |
| POST | `/reports/{reportId}/submit` | report.submit | Submitted state |
| POST | `/reports/{reportId}/request-revision` | report.review | Return with comments |
| POST | `/reports/{reportId}/reject` | report.review | Reject with reason |
| POST | `/reports/{reportId}/approve` | report.approve | Stepwise or final approval, ledger once |
| GET | `/projects/{projectId}/progress` | progress.read | Target/actual/dev pp, cutoff label |
| GET | `/projects/{projectId}/reports/weekly` | document.preview | Authoritative weekly preview |
| POST | `/projects/{projectId}/documents` | document.generate | Snapshot + async job |
| POST | `/documents/{documentId}/publish` | document.publish | Publish generated revision |
| GET | `/documents/{documentId}/download` | document.read | Authorized time-limited link |
| POST | `/integrations/telegram/link-token` | authenticated | Expiring one-time account link |
| POST | `/integrations/telegram/miniapp-session` | public + initData | Verify signed initData, bind user |
| POST | `/integrations/telegram/webhook` | Telegram secret | Dedupe updates, minimal ack |

## 3. Example payload contracts

### Create daily report

```json
{
  "report_date": "2026-10-24",
  "entries": [
    {
      "work_item_id": "<uuid>",
      "location_id": "<uuid>",
      "measure_kind": "quantity",
      "quantity_delta": "19.04",
      "notes": "Pengecoran beton selesai"
    }
  ],
  "attachment_ids": ["<uuid>"]
}
```

`measure_kind` expected values require ADR before schema; milestone and percent use typed distinct payloads, never `quantity_delta` filled with bogus volume.

### Approve report

```json
{
  "report_revision": 3,
  "comment": "Volume dan foto sesuai pemeriksaan"
}
```

Response on final approval includes `status: approved`, `approved_at`, `ledger_commit_id` or equivalent. On intermediate step, status can remain `under_review`. Replaying same idempotency key returns same logical result, not a second posting.

### Progress query

```json
{
  "project_id": "<uuid>",
  "as_of_date": "2026-10-24",
  "budget_version_id": "<uuid>",
  "baseline_id": "<uuid>",
  "planned_cumulative_pct": "75.00",
  "actual_cumulative_pct": "68.50",
  "deviation_pp": "-6.50",
  "updated_at": "2026-10-24T10:00:00Z"
}
```

Examples are schemas to implement; `<uuid>` placeholders are not valid UUID values.

## 4. API testing contract

Each endpoint gets positive + negative tenant / project membership tests; mutation endpoints get unauthorized, validation, duplicate request, stale version/race and not-found tests. OpenAPI will replace informal examples as implementation begins. Update docs and generated types with breaking changes.
