# Construction SaaS Backend

This repository contains backend implementation and product/domain documentation for Construction SaaS. The Go module is in `backend/`; see `README-DOCS.md` for the documentation index. Build and runtime details are evolving alongside the task backlog.

## Development

From `backend/`:

```sh
go test ./...
go vet ./...
go test ./internal/tenancy -run 'Company|Membership|Invitation' -count=1
```

For PostgreSQL-backed tests, set `TEST_DATABASE_URL` to a dedicated disposable database. Start local services with `docker compose up -d postgres minio` where Docker is available. The API runs on `:8080` by default (`API_ADDR` overrides it) and the API/worker need `DATABASE_URL`.

The API supports password login/logout, company creation/listing, company member listing, and company invitation creation/listing. Company creation creates its owner membership transactionally; company-scoped reads and invitations resolve active membership from the authenticated session. Project authorization and invitation acceptance are separate backlog work.

For browser deployments, set `APP_ENV=production` behind HTTPS. Configure a stable `CSRF_SECRET` of at least 32 bytes and comma-separated exact `TRUSTED_ORIGINS` to enable CSRF defense. Rate limits can be configured with `LOGIN_RATE_LIMIT_*` and `API_RATE_LIMIT_*`; current defaults are process-local, so production multi-replica deployment needs shared atomic rate-limit state.
