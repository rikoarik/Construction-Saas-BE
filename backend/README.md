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

Run the PostgreSQL integration test against a dedicated test database:

```sh
TEST_DATABASE_URL='postgres://app:app@localhost:54329/construction_saas_test?sslmode=disable' \
  go test ./internal/database -run TestMigratePostgres -count=1
```

Start a local PostgreSQL service with `docker compose up -d postgres` when Docker is available. Set `DATABASE_URL` for application components. The API listens on `:8080` by default; set `API_ADDR` to override it. The worker entry point currently only supports graceful shutdown; task BE-006 will implement outbox processing.
