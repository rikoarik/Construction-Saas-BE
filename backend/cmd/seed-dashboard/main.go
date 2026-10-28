// seed-dashboard fills a local database with demo data for the platform dashboard.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"construction-saas/backend/db/seeds"
	"construction-saas/backend/internal/database"
)

func main() {
	force := flag.Bool("force", false, "re-run the dataset to restore rows that were deleted")
	flag.Parse()
	if err := run(context.Background(), *force); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, force bool) error {
	if os.Getenv("APP_ENV") == "production" {
		return fmt.Errorf("refusing to seed demo data when APP_ENV=production")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	// Seeding needs the schema the dashboard reads; applying embedded migrations
	// keeps the command usable on a fresh local database.
	if err := database.Migrate(ctx, url); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	pool, err := database.Open(ctx, url)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	// Demo accounts are the marker for the whole dataset. Re-running without
	// -force is a no-op so an existing database is never rewritten.
	var seeded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM app.users WHERE email_normalized LIKE '%' || $1`, seeds.DemoAccountDomain).Scan(&seeded); err != nil {
		return fmt.Errorf("inspect existing demo data: %w", err)
	}
	if seeded > 0 && !force {
		fmt.Println("Demo data already present; nothing to do. Use -force to restore deleted rows.")
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("start transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, seeds.DashboardDemo); err != nil {
		return fmt.Errorf("apply demo dataset: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	var companies, users, failed int64
	if err := pool.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM app.companies),
	 (SELECT count(*) FROM app.users),
	 (SELECT count(*) FROM app.outbox_events WHERE status IN ('dead','pending') AND last_failed_at IS NOT NULL)`).Scan(&companies, &users, &failed); err != nil {
		return fmt.Errorf("summarize demo data: %w", err)
	}
	fmt.Printf("Demo data ready: %d companies, %d users, %d failed or retrying jobs.\n", companies, users, failed)
	return nil
}
