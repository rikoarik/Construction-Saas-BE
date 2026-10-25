# Database Design — PostgreSQL

Refer to `docs/product/BLUEPRINT.md` §9 for complete conceptual entities. This document governs implementation constraints, not full runnable migrations.

## Schema clusters

- **Identity:** `users`, `auth_sessions`, `companies`, `company_memberships`, `company_invitations`, `company_settings`.
- **Projects:** `projects`, `project_memberships`, `project_parties`, `contracts`, `contract_versions`, `work_items`, `units`, `project_locations`.
- **Budgets:** `budget_versions`, `budget_lines`, `import_jobs`, `import_mappings`, `plan_baselines`, `plan_weekly_allocations`, `report_periods`.
- **Reporting:** `daily_reports`, `daily_report_entries`, `daily_report_revisions`, `attachments`, `approval_steps`, `approval_events`, `progress_ledger`.
- **Delivery:** `report_templates`, `generated_documents`, `document_signoffs`, `audit_logs`, `telegram_links`, `telegram_link_tokens`, `telegram_processed_updates`, `notification_outbox`, `job_runs`.

## Isolation invariants

A `(project_id, company_id)` pair must refer to same company. Enforce via unique candidate key `(company_id,id)` on parent project and composite FK on children. Same for work_item, reports and budget lines where ownership needs cross-check. No query by standalone tenant-owned UUID without authorization. Authenticated user is resolved server-side; client-supplied company_id is selector, not proof.

## Durable records

- `progress_ledger`: insert-only corrections via `correction_of`. `source_entry_id` initial posting unique; correction posting has separate ID, reason and reviewer.
- `approval_events` and `audit_logs`: append-only. Restrict delete/update privileges for service roles where practical.
- Approved `budget_versions`, `plan_baselines`, and generated document snapshot: immutable. Use versions + effective dates, never in-place rewriting history.

## Numeric policies

Choose `NUMERIC(p,s)` based on money currency and quantities; preserve exact decimal semantics in Go (e.g., dedicated decimal library or scaled integer with explicit per-domain scale). Do not use float64 for financial truth. Keep full precision in DB and service; round display/export according to documented decimal policy. Test allocation sum ≈ 100% with defined residual assignment.

## Constraints + indexes

- unique `(company_id,user_id)` membership; unique `(company_id,project_code)`.
- unique `(budget_version_id,work_item_id)` for one budget line/item/version; unique `(baseline_id,work_item_id,week_start)`.
- unique `telegram_processed_updates.update_id`, unique outbox idempotency_key and ledger posting source.
- `CHECK(contract_qty >= 0)` and validation of items eligible_for_weight; validate leaf-only aggregation.
- Index `company_id`, `project_id`, `report_date`, `status`, ledger `effective_date`, active membership and periods.

## Zero-downtime strategy (future scale)

Expand: add nullable columns/new tables → deploy readers compatible with both → backfill in batches → enforce new constraints non-blockingly → switch readers/writers → contract old fields in later release. Audit lock durations, index concurrency and rollback feasibility. Any tenant migration must run trial restore and consistency checks.

## Data lifecycle

Define retention per company contract. Separate user identities from audit actor snapshot; anonymization should not destroy report history needed by contract. Backups plus object storage restore must be tested. Delete flows require explicit legal/data compliance review.
