# Data Dictionary & Logical Relational Schema

**Status:** Proposed design; bukan migrasi SQL executable. **Database:** PostgreSQL + goose + pgx/sqlc.  
**See:** [DATABASE](DATABASE.md), [SYSTEM-DESIGN](SYSTEM-DESIGN.md), [PRD](../product/PRD.md).

## 1. Konvensi fisik

- Primary key: `id UUID`, `created_at TIMESTAMPTZ`, `updated_at TIMESTAMPTZ` kecuali log immutable; domain identifiers (`project_code`, `work_code`) terpisah.
- Object tenant-owned: `company_id UUID NOT NULL`; object project-owned: `project_id UUID NOT NULL` dan `company_id UUID NOT NULL` untuk enforce scope/foreign key.
- Amount: `NUMERIC(p,s)` dengan scale per currency; quantity/weight: `NUMERIC` dengan presisi disepakati, tidak `float/double`.
- `status` memakai enum/check constraint terversi; timestamps disimpan UTC, pemetaan `DATE` periode berdasarkan zona waktu proyek.
- Composite key parent `UNIQUE(company_id,id)` dan project `UNIQUE(company_id,id)` agar FK anak `(company_id,project_id)` menolak cross-tenant association.
- `deleted_at` hanya untuk objek yang secara hukum aman di-soft-delete; ledger/audit/approved snapshot tidak boleh terhapus diam-diam.
- Jangan membuat semua field custom sebagai kolom arbitrer; gunakan validated JSONB untuk tambahan non-kalkulasi beserta `schema_version`.

## 2. Relational table catalog

| Table | Core columns | Mandatory relationships / constraints |
|---|---|---|
| `users` | id, email, password_hash, status | unique normalized email; password hash only |
| `auth_sessions` | id, user_id, token_hash, expires_at, revoked_at | FK users; token plaintext never stored |
| `companies` | id, name, slug, status | unique slug; separate company data |
| `company_memberships` | company_id, user_id, role, status | FK company/user; unique(company_id,user_id) |
| `company_invitations` | id, company_id, email, token_hash, expires_at, accepted_at | one-time token; scoped role |
| `company_settings` | company_id, timezone, locale, defaults_json | unique company_id |
| `projects` | id, company_id, project_code, name, project_type, timezone, status | unique(company_id,project_code), company FK |
| `project_memberships` | company_id, project_id, user_id, role, status | company membership check; unique(project_id,user_id) |
| `contracts` | id, company_id, project_id, contract_number, partner_name, status | project FK; current version resolved separately |
| `contract_versions` | id, contract_id, version_no, effective_date, approved_at, metadata_json | unique(contract_id,version_no); immutable when approved |
| `project_locations` | id, company_id, project_id, parent_id, name, location_type | same-project parent; e.g. floor/STA/zone |
| `units` | id, code, display_name, dimension | stable unit reference; custom mapped units reviewed |
| `work_items` | id, company_id, project_id, parent_id, work_code, name, unit_id, measure_method, is_leaf | same-project tree; no cycles; measurement typed |
| `budget_versions` | id, company_id, project_id, version_no, status, effective_date, approved_at | unique(project_id,version_no); immutable approved |
| `budget_lines` | id, company_id, project_id, budget_version_id, work_item_id, contracted_qty, unit_price, amount | unique(version,work_item); check amount policy |
| `import_jobs` | id, company_id, project_id, source_object_key, mapping_id, status, errors_json | source file private; staging result reviewed |
| `import_mappings` | id, company_id, name, sheet_config, field_mapping_json, version | scoped to company; schema validated |
| `plan_baselines` | id, company_id, project_id, budget_version_id, version_no, status, approved_at | unique(project_id,version_no); approved immutable |
| `plan_weekly_allocations` | id, company_id, baseline_id, work_item_id, week_start, contribution_pct | unique(baseline,work_item,week); period valid |
| `daily_reports` | id, company_id, project_id, reporter_id, report_date, status, revision_no, workflow_version | state and version for optimistic concurrency |
| `daily_report_entries` | id, company_id, project_id, report_id, work_item_id, location_id, measure_kind, qty_delta, progress_point_delta, custom_data_json | exactly one measurement kind; constraints TBD ADR |
| `daily_report_revisions` | id, company_id, report_id, revision_no, changed_by, reason, snapshot_json | preserve historic submitted content |
| `attachments` | id, company_id, project_id, report_id, object_key, mime_type, byte_size, checksum | no public buckets; content may need scanning |
| `approval_steps` | id, company_id, project_id, step_order, role_or_reviewer, workflow_version | versioned workflow levels; effective version tracked |
| `approval_events` | id, company_id, report_id, reviewer_id, action, comment, created_at, idempotency_key | append only; idempotency guard |
| `progress_ledger` | id, company_id, project_id, work_item_id, source_entry_id, measure_kind, qty_delta, progress_point_delta, effective_date, correction_of, created_at | append only; one base posting/source entry; typed events; unique correction key |
| `report_periods` | id, company_id, project_id, start_date, end_date, timezone, status | no hidden UTC shifting; immutable after publish |
| `report_templates` | id, company_id, template_type, version, configuration_json, status | global defaults possible via owner scope; immutable on publish |
| `generated_documents` | id, company_id, project_id, period_id, template_id, template_version, budget_version_id, baseline_id, ledger_cutoff, snapshot_hash, format, object_key, status, revision_no | snapshot refs fixed; signed download authorization |
| `document_signoffs` | id, company_id, document_id, actor_id, action, signed_at | track review/publish, not legal certified signature |
| `telegram_links` | id, user_id, telegram_user_id, linked_at, revoked_at | verify link ownership; unique active telegram user |
| `telegram_link_tokens` | id, user_id, token_hash, expires_at, consumed_at | one-time expiring link |
| `telegram_processed_updates` | update_id, received_at, processed_at, status | unique update_id; replay safe |
| `notification_outbox` | id, company_id, type, payload_json, idempotency_key, status, next_attempt_at | unique idempotency key; retryable |
| `job_runs` | id, company_id, job_kind, reference_id, attempts, state, last_error | no secrets or raw PII in logs |
| `audit_logs` | id, company_id, project_id, actor_id, action, entity_type, entity_id, occurred_at, metadata_json | append only, limited metadata |

Not every `company_id` belongs on global reference tables (e.g., `units`). `company_id` on `telegram_links` depends on one-link-per-user or per-tenant policy; user identity remains global but permissions are resolved per company/project each request. Review normalization before migrations.

## 3. Uniqueness, foreign keys, indexes

- `projects UNIQUE(company_id,project_code)` + candidate `UNIQUE(company_id,id)`.
- Tenant-owned child `FOREIGN KEY(company_id,project_id) REFERENCES projects(company_id,id)`.
- Reports/items/budget lines use composite relationships back to **their own project**: a valid work item from project X cannot be added to project Y.
- `budget_lines UNIQUE(budget_version_id,work_item_id)` and FK checks budget and item same company/project.
- `plan_weekly_allocations UNIQUE(baseline_id,work_item_id,week_start)` and `contribution_pct >= 0`.
- `approval_events` unique idempotency/action key; `progress_ledger` unique initial posting by source entry (partial unique index if correction semantics require).
- `notification_outbox UNIQUE(idempotency_key)`; `telegram_processed_updates UNIQUE(update_id)`.
- Index common filters `(company_id,project_id,report_date)`, `(company_id,project_id,status)`, ledger `(company_id,project_id,effective_date)`.

## 4. Typed progress event caveat

Current blueprint describes `progress_ledger.qty_delta`, fine for volume-based work. **But milestone / verified-percent methods cannot be modeled only by qty.** Proposed physical design uses `measure_kind` enum + one of `qty_delta` or `progress_point_delta`, with CHECK and normalized per-method interpretation. Before migration, write ADR and fixture scenarios for baseline, stage completion, reversal and method changes. Never invent fake physical quantity for percentage.

## 5. Data lineage & revisions

`daily_report_entry → approval_event → progress_ledger → period snapshot → generated_document(binary)`.

- Approvals append events; base ledger posting has unique source entry.
- Corrections append a reversal/delta with `correction_of` and explicit reason.
- Approved RAB and baseline never mutate; addendum creates new versions.
- Every published document saves version IDs, ledger cutoff, exact snapshot hash and binary path.
- If old report requires correction, issue a new revision and retain old artifact under retention policy.

## 6. Cross-version calculations — unresolved

Quantities measured before an addendum can be valued differently according to contract terms. Define explicit cutover/effective-price policy and approved normalization into reporting snapshots before implementing cross-version reports. Do not recalculate historical paid/earned values under the latest price without approval.

## 7. Migration deliverables

Physical implementation via `backend/db/migrations/*.sql` (goose): `up`, safe `down` only where feasible, indexes, FKs and constraints. Complement with `backend/db/queries/*.sql` for sqlc and integration tests under real PostgreSQL. This file documents intent, not finished DDL.
