package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPlatformSessionMissingCookie(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAuthRoutes(mux, &AuthService{}, false)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/platform/session", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", res.Code)
	}
}

func TestPlatformSessionGrantRevocationAndDisabledUser(t *testing.T) {
	store, user := integrationIdentity(t)
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM app.platform_admins WHERE user_id=$1::uuid`, user.ID)
	})
	ctx := context.Background()
	token := "platform-test-token"
	_, err := store.CreateSession(ctx, user.ID, sessionTokenHash(token), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterAuthRoutes(mux, &AuthService{Store: store}, false)
	check := func(expected int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/platform/session", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != expected {
			t.Fatalf("status = %d, want %d", res.Code, expected)
		}
	}
	check(http.StatusForbidden)
	if _, err := store.pool.Exec(ctx, `INSERT INTO app.platform_admins (user_id) VALUES ($1::uuid)`, user.ID); err != nil {
		t.Fatal(err)
	}
	// A platform grant alone stays fail-closed until the session records a
	// completed MFA verification.
	check(http.StatusForbidden)
	if _, err := store.pool.Exec(ctx, `UPDATE app.auth_sessions SET mfa_verified_at=now() WHERE token_hash=$1`, sessionTokenHash(token)); err != nil {
		t.Fatal(err)
	}
	check(http.StatusOK)
	if _, err := store.pool.Exec(ctx, `DELETE FROM app.platform_admins WHERE user_id=$1::uuid`, user.ID); err != nil {
		t.Fatal(err)
	}
	check(http.StatusForbidden)
	if _, err := store.pool.Exec(ctx, `INSERT INTO app.platform_admins (user_id) VALUES ($1::uuid)`, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE app.users SET status='disabled' WHERE id=$1::uuid`, user.ID); err != nil {
		t.Fatal(err)
	}
	check(http.StatusUnauthorized)
}
