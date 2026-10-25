# Company and Membership Schema (BE-013)

BE-013 adds companies, company memberships, one-to-one company settings, and company-scoped invitations. User identity remains global; tenant access derives from an active membership. Company creation atomically creates the company, its initial owner membership, settings, and an audit event. The initial schema accepts company statuses `active` and `suspended`; membership roles `owner`, `admin`, `member`, `viewer`; and membership statuses `active`, `suspended`, `removed`.

Owners and admins may list members and create/list invitations; other active members cannot. Invitation roles are limited to `admin`, `member`, and `viewer`. Only a hash of the random invitation token is stored; the plaintext bearer token is returned on creation only. The current HTTP handler assigns a seven-day expiry. Acceptance, revocation, resend, and pending-invitation uniqueness are not implemented because their lifecycle contract is unspecified. Project-level authorization belongs to BE-014.

Database-backed tests use `TEST_DATABASE_URL` and must target a disposable database:

```sh
TEST_DATABASE_URL='postgres://app:app@localhost:54329/construction_saas_test?sslmode=disable' \
  go test ./internal/tenancy -count=1
```
