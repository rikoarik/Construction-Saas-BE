// create-superadmin provisions a new platform administrator using direct database access.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/mail"
	"os"
	"strings"

	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/identity"
)

func main() {
	email := flag.String("email", "", "email address for a new platform administrator")
	flag.Parse()
	if err := run(*email); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(email string) error {
	email = strings.TrimSpace(email)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return fmt.Errorf("a valid email address is required")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	// Read the password from stdin, not an argument or environment variable.
	password, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(password) == 0 {
		return fmt.Errorf("read password from stdin")
	}
	password = strings.TrimSuffix(strings.TrimSuffix(password, "\n"), "\r")
	if !identity.ValidNewPassword(password) {
		return fmt.Errorf("password must contain at least 8 characters and at most 1024 bytes")
	}
	hash, err := (identity.PasswordHasher{}).Hash(password)
	if err != nil {
		return err
	}
	ctx := context.Background()
	pool, err := database.Open(ctx, url)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("start transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO app.users (email, email_normalized, password_hash, status)
		VALUES ($1, $2, $3, 'active') RETURNING id::text`, email, identity.NormalizeEmail(email), hash).Scan(&id)
	if err != nil {
		return fmt.Errorf("create new user (email must not already exist): %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.platform_admins (user_id) VALUES ($1::uuid)`, id); err != nil {
		return fmt.Errorf("grant platform access: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	fmt.Println("Platform administrator created.")
	return nil
}
