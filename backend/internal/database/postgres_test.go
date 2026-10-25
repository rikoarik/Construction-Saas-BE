package database

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPoolConfigRequiresDatabaseURL(t *testing.T) {
	if _, err := poolConfig(""); err == nil {
		t.Fatal("poolConfig with empty URL succeeded")
	}
}

func TestPoolConfigRejectsInvalidDatabaseURL(t *testing.T) {
	if _, err := poolConfig("not-a-postgres-url"); err == nil {
		t.Fatal("poolConfig with invalid URL succeeded")
	}
}

func TestPoolConfig(t *testing.T) {
	config, err := poolConfig("postgres://user:pass@localhost/app")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := config.MaxConnIdleTime, 5*time.Minute; got != want {
		t.Fatalf("MaxConnIdleTime = %s, want %s", got, want)
	}
	if got, want := config.ConnConfig.RuntimeParams["application_name"], "construction-saas-backend"; got != want {
		t.Fatalf("application_name = %q, want %q", got, want)
	}
	if got, want := config.ConnConfig.RuntimeParams["search_path"], "app,public"; got != want {
		t.Fatalf("search_path = %q, want %q", got, want)
	}
}

func TestPoolConfigRespectsCustomSearchPath(t *testing.T) {
	config, err := poolConfig("postgres://user:pass@localhost/app?search_path=custom")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := config.ConnConfig.RuntimeParams["search_path"], "custom"; got != want {
		t.Fatalf("search_path = %q, want %q", got, want)
	}
}

func TestOpenFailsForUnreachableDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	pool, err := Open(ctx, "postgres://user:pass@127.0.0.1:1/app")
	if err == nil {
		pool.Close()
		t.Fatal("Open with unreachable PostgreSQL succeeded")
	}
}

func TestMigrateRequiresDatabaseURL(t *testing.T) {
	if err := Migrate(context.Background(), ""); err == nil {
		t.Fatal("Migrate with empty URL succeeded")
	}
}

func TestMigratePostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL pool: %v", err)
	}
	defer pool.Close()

	var result int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&result); err != nil {
		t.Fatalf("query PostgreSQL: %v", err)
	}
	if result != 1 {
		t.Fatalf("SELECT 1 = %d, want 1", result)
	}

	if err := Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}

	var schemaExists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'app')").Scan(&schemaExists); err != nil {
		t.Fatalf("check baseline schema: %v", err)
	}
	if !schemaExists {
		t.Fatal("baseline migration did not create app schema")
	}
}
