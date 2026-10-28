# Company Admin Backend Tasks

**Owner:** backend-core · This is a focused delivery view, not a replacement for `BACKEND-TASKS.md`. Status is scoped to the current `chore/be-014-project-permissions` working tree; other branches are not integrated evidence. Canonical API changes require review before frontend implementation. Acceptance: `docs/backend/BACKEND-ACCEPTANCE.md`, especially BE-AC-001 and BE-AC-002.

## Current baseline

Company creation/listing, admin member listing, and invitation creation/listing are implemented in `backend/internal/tenancy`; invitation tokens are hashed and creation is audited. Project permission checks exist in `backend/internal/projects/authorization.go`, not project CRUD on this branch. Invitation acceptance and member management are not implemented. Invitation authorization locks the active membership during creation; separately owned notification changes in the working tree are not release evidence. PostgreSQL-backed tests require a dedicated `TEST_DATABASE_URL`.

## Ordered work

| ID | Status | Scope and acceptance | Dependencies / evidence |
|---|---|---|---|
| BE-CA-001 | verified | Reject malformed invitation email input before any database write; accept a single bare mailbox, reject display names, lists and header injection. | `backend/internal/tenancy/invitation_email.go`, `store.go`, `invitation_email_test.go`; unit tests. Does not claim end-to-end invitation lifecycle. |
| BE-CA-002 | verified | Verify the invitation authorization transaction lock against concurrent membership suspension; preserve 404 for nonmembers and 403 for insufficient active roles. | `backend/internal/tenancy/store.go`, `invitation_authorization_test.go`, and `store_test.go`; PostgreSQL-backed `go test -race ./... -count=1` passed, including role and cross-tenant negatives. |
| BE-CA-003 | planned | Define and review invitation acceptance contract and policy: matching authenticated identity, expiry, replay/concurrency, existing or suspended membership, audit and safe token handling. Implement atomic acceptance only after approval. | BE-CA-002; BE-013; contract review, PostgreSQL transaction and race tests. |
| BE-CA-004 | planned | Define member role/status changes and last-owner protection; enforce tenant-scoped authorization, transactional audit, concurrent changes and no escalation. | BE-CA-002; reviewed role policy and API contract; BE-AC-001/002 integration tests. |
| BE-CA-005 | planned | Company settings read/update with validated timezone and audited owner/admin permission; specify allowed settings and concurrency policy first. | BE-013, BE-024; reviewed contract and tenant/role tests. |
| BE-CA-006 | planned | Paginate company members and invitations without silently truncating results; specify stable cursors and avoid exposing token hashes. | BE-013; reviewed API contract; pagination, tenant and role tests. |
| BE-CA-007 | blocked | Company project administration endpoints and team assignment, including project/company scoping. | BE-020 and BE-023 integration; these exist as separate work, not on this branch; contract and cross-tenant tests. |
| BE-CA-008 | blocked | Company subscription/usage visibility and enforcement for members/projects. | BE-100, BE-102, BE-103 integrated and contract reviewed; bypass-negative tests. |

`verified` means the stated acceptance checks passed in this working tree, not that code is committed, deployed, or all related backlog tasks are complete.
