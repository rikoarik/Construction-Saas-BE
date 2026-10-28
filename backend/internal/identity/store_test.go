package identity

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"construction-saas/backend/internal/database"
)

func TestNormalizeEmail(t *testing.T) {
	if got, want := NormalizeEmail("  Alice.Example@Example.COM "), "alice.example@example.com"; got != want {
		t.Fatalf("NormalizeEmail = %q, want %q", got, want)
	}
}

func TestCreateUserValidatesInputs(t *testing.T) {
	if _, err := NewStore(nil); err == nil {
		t.Fatal("NewStore(nil) succeeded")
	}
	store := &Store{}
	for _, tc := range []struct{ email, hash, status string }{
		{"", "hash", "active"}, {"x@y", "", "active"}, {"x@y", "hash", ""}, {"  ", "hash", "active"},
	} {
		if _, err := store.CreateUser(context.Background(), tc.email, tc.hash, tc.status); err == nil {
			t.Errorf("CreateUser(%q, %q, %q) succeeded", tc.email, tc.hash, tc.status)
		}
	}
}

func TestIdentityQueriesPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pool.Close()
	store, err := NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}

	email := "identity-test-" + time.Now().Format("20060102150405.000000000") + "@example.invalid"
	user, err := store.CreateUser(ctx, "  "+email+" ", "$test$argon2id-hash", "active")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM app.auth_sessions WHERE user_id=$1::uuid`, user.ID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM app.users WHERE id=$1::uuid`, user.ID)
	})
	if user.PasswordHash == email || user.EmailNormalized != NormalizeEmail(email) {
		t.Fatalf("persisted user has wrong normalized email/hash: %+v", user)
	}

	found, err := store.GetUserByNormalizedEmail(ctx, strings.ToUpper(email))
	if err != nil {
		t.Fatalf("lookup normalized email: %v", err)
	}
	if found.ID != user.ID || found.PasswordHash != user.PasswordHash {
		t.Fatalf("lookup user = %+v, want original user", found)
	}
	if _, err := store.CreateUser(ctx, strings.ToUpper(email), "$different$hash", "active"); err == nil {
		t.Fatal("duplicate normalized email succeeded")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Fatalf("duplicate email error = %v, want unique violation", err)
		}
	}

	rawToken := "raw-session-token-that-must-not-be-persisted"
	tokenHash := sessionTokenHash("test-session:" + email)
	session, err := store.CreateSession(ctx, user.ID, tokenHash, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.TokenHash != tokenHash {
		t.Fatalf("session hash = %q, want token hash", session.TokenHash)
	}
	active, err := store.GetActiveSessionByTokenHash(ctx, tokenHash)
	if err != nil || active.ID != session.ID {
		t.Fatalf("lookup active session = %+v, %v", active, err)
	}
	var rawCount, hashCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='app' AND table_name='auth_sessions' AND column_name='token_hash'`).Scan(&rawCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM app.auth_sessions WHERE token_hash=$1`, rawToken).Scan(&hashCount); err != nil {
		t.Fatal(err)
	}
	if rawCount != 1 || hashCount != 0 {
		t.Fatalf("plaintext token storage indicators: token columns=%d, raw token matches=%d", rawCount, hashCount)
	}
	if _, err := store.CreateSession(ctx, user.ID, tokenHash, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("duplicate token hash succeeded")
	}
	if _, err := store.CreateSession(ctx, "00000000-0000-0000-0000-000000000000", "orphan-hash", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("session with nonexistent user succeeded")
	}
	if _, err := store.RevokeSession(ctx, user.ID, session.ID); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err := store.GetActiveSessionByTokenHash(ctx, tokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked session lookup = %v, want ErrNotFound", err)
	}
}
