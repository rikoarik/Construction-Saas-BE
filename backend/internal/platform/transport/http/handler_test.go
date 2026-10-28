package platformhttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/httpapi"
	"construction-saas/backend/internal/identity"
	"construction-saas/backend/internal/platform/app"
	platformpg "construction-saas/backend/internal/platform/infra/postgres"
)

func TestDashboardAuthorizationAndConcurrentRequests(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := database.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store, err := identity.NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(ctx, "dashboard-auth-20261028@example.invalid", "hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Exec(ctx, `DELETE FROM app.platform_role_assignments WHERE user_id=$1`, user.ID)
		pool.Exec(ctx, `DELETE FROM app.auth_sessions WHERE user_id=$1`, user.ID)
		pool.Exec(ctx, `DELETE FROM app.users WHERE id=$1`, user.ID)
	}()
	token := "dashboard-auth-session"
	digest := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(digest[:])
	if _, err := store.CreateSession(ctx, user.ID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Handler{Auth: &identity.AuthService{Store: store}, Service: app.Service{Read: platformpg.DashboardRead{Pool: pool}}}.Register(mux)
	handler := httpapi.NewRouter(nil, mux, httpapi.SecurityConfig{})
	paths := []string{"/api/v1/platform/me", "/api/v1/platform/dashboard", "/api/v1/platform/dashboard/company-growth", "/api/v1/platform/dashboard/company-status", "/api/v1/platform/dashboard/activities", "/api/v1/platform/dashboard/subscription-distribution", "/api/v1/platform/dashboard/storage-usage", "/api/v1/platform/dashboard/integration-health", "/api/v1/platform/companies", "/api/v1/platform/jobs", "/api/v1/platform/notifications"}
	check := func(path string, cookie bool, expected int) []byte {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		if cookie {
			req.AddCookie(&http.Cookie{Name: "session", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != expected {
			t.Fatalf("%s: status %d want %d: %s", path, w.Code, expected, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store")
		}
		return w.Body.Bytes()
	}
	for _, path := range paths {
		check(path, false, 401)
		check(path, true, 403)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app.platform_role_assignments (user_id,role_code) VALUES ($1,'superadmin')`, user.ID); err != nil {
		t.Fatal(err)
	}
	check(paths[1], true, 403) // MFA not yet verified
	if _, err := pool.Exec(ctx, `UPDATE app.auth_sessions SET mfa_verified_at=now() WHERE token_hash=$1`, hash); err != nil {
		t.Fatal(err)
	}
	// Payload may contain tenant/private delivery data. Read projections must not
	// echo it through either jobs or the activity stream.
	const privateCanary = "PRIVATE_TENANT_PAYLOAD_DASHBOARD_CANARY"
	var canaryJobID int64
	if err := pool.QueryRow(ctx, `INSERT INTO app.outbox_events (event_type,payload,idempotency_key,status,last_failed_at) VALUES ('email.delivery',$1::jsonb,$2,'dead',now()) RETURNING id`, `{"secret":"`+privateCanary+`"}`, "dashboard-canary-20261028").Scan(&canaryJobID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM app.outbox_events WHERE id=$1`, canaryJobID)
	for _, path := range paths {
		body := check(path, true, 200)
		if strings.Contains(string(body), privateCanary) {
			t.Fatalf("private outbox payload leaked through %s", path)
		}
		if !json.Valid(body) {
			t.Fatalf("non-JSON response: %s", path)
		}
	}
	var dashboard app.Dashboard
	if err := json.Unmarshal(check(paths[1], true, 200), &dashboard); err != nil {
		t.Fatal(err)
	}
	// The storage payload must always carry the measured part, even when nothing
	// has been metered yet: the client distinguishes null from zero.
	var storage map[string]json.RawMessage
	if err := json.Unmarshal(check(paths[6], true, 200), &storage); err != nil {
		t.Fatal(err)
	}
	if _, ok := storage["api_requests"]; !ok {
		t.Fatalf("storage payload is missing api_requests: %v", storage)
	}
	if dashboard.Companies.Value == nil || dashboard.Users.Value == nil || dashboard.ActiveProjects.Value == nil || dashboard.ActiveProjects.Previous != nil {
		t.Fatalf("metrics: %+v", dashboard)
	}
	check(paths[1]+"?range=bad", true, 400)
	check("/api/v1/platform/jobs?status=delivered", true, 400)
	check("/api/v1/platform/jobs?status=", true, 400)
	check("/api/v1/platform/jobs?status=failed,failed", true, 400)
	check("/api/v1/platform/companies?limit=101", true, 400)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); check(paths[1], true, 200) }()
	}
	wg.Wait()
	if _, err := pool.Exec(ctx, `DELETE FROM app.platform_role_assignments WHERE user_id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	check(paths[1], true, 403)
	if _, err := pool.Exec(ctx, `INSERT INTO app.platform_admins (user_id) VALUES ($1)`, user.ID); err != nil {
		t.Fatal(err)
	}
	check(paths[1], true, 200) // legacy provisioned superadmin retains dashboard access
	var operator struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(check(paths[0], true, 200), &operator); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(operator.Capabilities, "platform.company.read") {
		t.Fatalf("legacy superadmin /me missing company navigation capability: %v", operator.Capabilities)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM app.platform_admins WHERE user_id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE app.users SET status='disabled' WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	check(paths[1], true, 401)
}
