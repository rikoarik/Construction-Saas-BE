package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestChangePasswordIntegration(t *testing.T) {
	store, user := integrationIdentity(t)
	ctx := context.Background()
	h := PasswordHasher{}
	old := "the old password is lengthy"
	next := "a different long password"
	hash, err := h.Hash(old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE app.users SET password_hash=$2 WHERE id=$1::uuid`, user.ID, hash); err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(ctx, user.ID, sessionTokenHash("password-change-token"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateSession(ctx, user.ID, sessionTokenHash("password-change-other"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	service := &AuthService{Store: store}
	if err = service.ChangePassword(ctx, session, "wrong", next); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("wrong password: %v", err)
	}
	if err = service.ChangePassword(ctx, session, old, "short"); !errors.Is(err, ErrPasswordPolicy) {
		t.Fatalf("password policy: %v", err)
	}
	if err = service.ChangePassword(ctx, session, old, next); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"password-change-token", "password-change-other"} {
		if _, err = store.GetActiveSessionByTokenHash(ctx, sessionTokenHash(token)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("session not revoked: %v", err)
		}
	}
	updated, err := store.GetUserByNormalizedEmail(ctx, user.Email)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := h.Verify(updated.PasswordHash, next)
	if err != nil || !valid {
		t.Fatalf("new password: %v", err)
	}
	if err = store.CreateSessionForPassword(ctx, user.ID, hash, sessionTokenHash("old-login"), time.Now().Add(time.Hour)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("stale password created session: %v", err)
	}
}
