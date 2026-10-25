# Construction SaaS Backend Scaffold

The backend implementation lives in this directory as a Go module. The API and worker are separate entry points of the proposed modular monolith; domain modules, PostgreSQL infrastructure, migrations, and application ports are introduced by their dependency-ordered tasks.

## Development commands

Run from this directory:

```sh
go test ./...
go vet ./...
go run ./cmd/api
go run ./cmd/worker
```

The API listens on `:8080` by default; set `API_ADDR` to override it. The worker entry point currently only supports graceful shutdown; task BE-006 will implement outbox processing.
