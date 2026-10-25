# Identity Schema (BE-010)

BE-010 introduces the `app.users` and `app.auth_sessions` tables and simple SQL queries in `db/queries/identity.sql`. Email normalization for the initial implementation trims surrounding spaces and lowercases the address; it does not perform provider-specific alias rewriting. This is the policy enforced by `internal/identity.NormalizeEmail` and the migration check.

`password_hash` stores only a password-hash value. `auth_sessions.token_hash` stores the opaque session-token hash, never the plaintext token; expiration and revocation are persisted. Login, Argon2id parameter selection, cookie issuance, session renewal policy, and CSRF behavior belong to later tasks.

Database integration tests use `TEST_DATABASE_URL` and must target a disposable test database. Run them with:

```sh
TEST_DATABASE_URL='postgres://app:app@localhost:54329/construction_saas_test?sslmode=disable' \
  go test ./internal/identity -count=1
```

SQL query definitions are configured for sqlc, but generation was not verified in this local environment because the sqlc binary is not installed; keep generated query code out of sync until the approved generator toolchain is available.
