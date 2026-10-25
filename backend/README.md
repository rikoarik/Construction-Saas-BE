# Construction SaaS Backend Scaffold

The backend implementation lives in this directory as a Go module. The API and worker are separate entry points of the proposed modular monolith; domain modules, PostgreSQL infrastructure, migrations, and application ports are introduced by their dependency-ordered tasks.

## Development commands

Run from this directory:

```sh
go test ./...
go test ./internal/database -run TestPoolConfig
go vet ./...
cd .. && ./tools/check_api_contract.sh
cd backend && go run ./cmd/api
go run ./cmd/worker
```

Run PostgreSQL integration tests against a dedicated test database:

```sh
TEST_DATABASE_URL='postgres://app:app@localhost:54329/construction_saas_test?sslmode=disable' \
  go test ./internal/identity -count=1
```

Start a local PostgreSQL service with `docker compose up -d postgres` when Docker is available. Set `DATABASE_URL` for application components. The API listens on `:8080` by default; set `API_ADDR` to override it. Set `APP_ENV=production` behind HTTPS for Secure cookies and HSTS.

## Security middleware

Set a stable `CSRF_SECRET` of at least 32 bytes and comma-separated exact origins in `TRUSTED_ORIGINS` to enable CSRF protection. Authenticated browser clients obtain a session-bound token from `/api/v1/auth/csrf` and send it as `X-CSRF-Token` on unsafe cookie-authenticated methods. Login requires a configured matching Origin/Referer.

Configure `LOGIN_RATE_LIMIT_CAPACITY`, `LOGIN_RATE_LIMIT_WINDOW`, `LOGIN_RATE_LIMIT_MAX_KEYS` and matching `API_RATE_LIMIT_*` variables. Defaults are 10 login/minute and 120 general API requests/minute per socket peer. Forwarded headers are ignored. The built-in limiter is process-local; production replicas require shared atomic state.

The worker processes the PostgreSQL outbox using leased claims, retries, and dead-letter tracking. Register a concrete `outbox.Handler` for each event type before production use; unhandled events retry and eventually dead-letter.
