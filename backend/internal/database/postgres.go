package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"construction-saas/backend/db/migrations"
)

var migrationFS = migrations.FS

func poolConfig(databaseURL string) (*pgxpool.Config, error) {
	if databaseURL == "" {
		return nil, errors.New("database URL is required")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	config.MaxConnIdleTime = 5 * time.Minute
	config.ConnConfig.RuntimeParams["application_name"] = "construction-saas-backend"
	if config.ConnConfig.RuntimeParams["search_path"] == "" {
		config.ConnConfig.RuntimeParams["search_path"] = "app,public"
	}
	return config, nil
}

// Open connects to PostgreSQL and verifies the connection with a bounded ping.
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := poolConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return pool, nil
}

// Migrate applies embedded Goose migrations to the database URL.
func Migrate(ctx context.Context, databaseURL string) error {
	if databaseURL == "" {
		return errors.New("database URL is required")
	}

	db, err := goose.OpenDBWithDriver("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	goose.SetBaseFS(migrationFS)
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
