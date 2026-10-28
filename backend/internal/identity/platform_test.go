package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLegacySuperadminReceivesRoleCapabilitiesWithoutTenantAccess(t *testing.T) {
	store, user := integrationIdentity(t)
	ctx := context.Background()
	if _, err := store.pool.Exec(ctx, `INSERT INTO app.platform_admins (user_id) VALUES ($1::uuid)`, user.ID); err != nil {
		t.Fatal(err)
	}
	capabilities, err := store.PlatformCapabilities(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"platform.access", "platform.dashboard.read", "platform.company.read", "platform.company.create", "platform.member.read"} {
		found := false
		for _, capability := range capabilities {
			if capability == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("legacy superadmin missing %s: %v", required, capabilities)
		}
	}
	for _, capability := range capabilities {
		if capability == "company.read" || capability == "project.read" {
			t.Errorf("platform grant must not imply tenant capability %s", capability)
		}
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM app.platform_admins WHERE user_id=$1::uuid`, user.ID); err != nil {
		t.Fatal(err)
	}
	capabilities, err = store.PlatformCapabilities(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(capabilities) != 0 {
		t.Errorf("revoked legacy grant still has capabilities: %v", capabilities)
	}
}

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
