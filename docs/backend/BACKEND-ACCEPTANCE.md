# Backend Core Acceptance Criteria

These criteria cover Go backend core, PostgreSQL, calculations, authorization, documents, storage, workers, and provider-neutral notifications. Telegram-provider behavior is validated separately in `BOT-ACCEPTANCE.md`.

| ID | Scenario | Pass condition |
|---|---|---|
| BE-AC-001 | tenant isolation | company A cannot read/write company B data by guessed IDs, API, export, or storage path |
| BE-AC-002 | scoped roles | a user may have different company/project roles and unauthorized action is denied server-side |
| BE-AC-003 | WBS/project flexibility | building and road fixtures work without hardcoded project-type forks |
| BE-AC-004 | RAB import | alternate spreadsheet columns map to canonical data and bad rows block finalization with precise errors |
| BE-AC-005 | budget immutability | approved budget cannot mutate; addendum creates a new version with history retained |
| BE-AC-006 | baseline versioning | approved plan is immutable and rebaseline preserves historical comparison |
| BE-AC-007 | report state machine | draft/submit/revision/reject/approve transitions obey role and state rules |
| BE-AC-008 | approval idempotency | concurrent/retried final approval posts one logical ledger effect per source entry |
| BE-AC-009 | quantity regression | target 82.04 m3 with approved 63.00 + 19.04 produces 82.04 cumulative and 100.00% item progress |
| BE-AC-010 | deviation semantics | plan 75.00%, actual 68.50% yields -6.50 percentage points |
| BE-AC-011 | unapproved data | submitted-only measurements do not affect official progress |
| BE-AC-012 | report snapshot | XLSX/PDF use immutable source/version/period snapshot and remain reproducible after later addenda |
| BE-AC-013 | private media | unauthorized signed URL request fails; authorized URL is scoped and expires |
| BE-AC-014 | cost separation | earned work value is not silently treated as actual operational expenditure |
| BE-AC-015 | material ledger | derived stock reconciles to append-only movement history |
| BE-AC-016 | precise arithmetic | authoritative money/quantity/weight calculations avoid float drift and respect documented rounding |
| BE-AC-017 | correction | corrections/reversals remain auditable and never rewrite historical ledger silently |
| BE-AC-018 | entitlements | backend rejects disallowed plan feature even when client bypasses hidden UI |
| BE-AC-019 | outbox/jobs | retry does not duplicate durable side effects and dead-letter behavior is observable |
| BE-AC-020 | backup/recovery | documented restore drill reconstructs consistent DB/object state according to policy |
