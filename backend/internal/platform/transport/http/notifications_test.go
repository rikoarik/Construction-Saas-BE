package platformhttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/httpapi"
	"construction-saas/backend/internal/identity"
	"construction-saas/backend/internal/platform/app"
	platformpg "construction-saas/backend/internal/platform/infra/postgres"
)

// notificationFixture creates an MFA-verified superadmin with a live session, plus
// one suspended company so the derived feed is never empty, and returns a request
// helper bound to that session.
func notificationFixture(t *testing.T) (http.Handler, string, func(method, path, body string) *httptest.ResponseRecorder) {
	t.Helper()
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
	t.Cleanup(pool.Close)
	store, err := identity.NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(ctx, "notification-operator-"+time.Now().Format("150405.000000000")+"@example.invalid", "hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	// One suspended company guarantees a derived notification, so the feed and the
	// read markers are exercised instead of skipped on an otherwise empty database.
	slug := "notification-fixture-" + strings.ToLower(time.Now().Format("150405.000000000")) + "-" + user.ID[:8]
	var companyID string
	if err := pool.QueryRow(ctx, `INSERT INTO app.companies (name,slug,status) VALUES ('Notification Fixture',$1,'suspended') RETURNING id::text`, slug).Scan(&companyID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM app.platform_notification_reads WHERE user_id=$1`, user.ID)
		pool.Exec(ctx, `DELETE FROM app.platform_role_assignments WHERE user_id=$1`, user.ID)
		pool.Exec(ctx, `DELETE FROM app.auth_sessions WHERE user_id=$1`, user.ID)
		pool.Exec(ctx, `DELETE FROM app.companies WHERE id=$1`, companyID)
		pool.Exec(ctx, `DELETE FROM app.users WHERE id=$1`, user.ID)
	})
	token := "notification-session-" + user.ID
	digest := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(digest[:])
	if _, err := store.CreateSession(ctx, user.ID, hash, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app.platform_role_assignments (user_id,role_code) VALUES ($1,'superadmin')`, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE app.auth_sessions SET mfa_verified_at=now() WHERE token_hash=$1`, hash); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Handler{Auth: &identity.AuthService{Store: store}, Service: app.Service{Read: platformpg.DashboardRead{Pool: pool}}}.Register(mux)
	handler := httpapi.NewRouter(nil, mux, httpapi.SecurityConfig{})
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var reader *strings.Reader
		if body == "" {
			reader = strings.NewReader("")
		} else {
			reader = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, reader)
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	return handler, token, call
}

func TestNotificationReadMarkers(t *testing.T) {
	handler, _, call := notificationFixture(t)
	_ = handler

	// The feed is derived from real records, so the shared test database may hold
	// entries from other packages. Assert on the effect of the marker instead of an
	// absolute count.
	list := func(query string) app.NotificationPage {
		t.Helper()
		w := call("GET", "/api/v1/platform/notifications"+query, "")
		if w.Code != 200 {
			t.Fatalf("GET %s: %d %s", query, w.Code, w.Body.String())
		}
		var page app.NotificationPage
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}

	all := list("")
	if all.Items == nil {
		t.Fatal("items must be an array, not null")
	}
	if all.Type != "" || all.Read != "" {
		t.Fatalf("echoed filter: %+v", all)
	}
	if all.Unread < int64(len(all.Items)) && len(all.Items) > 0 {
		// Items are capped by limit, so unread can legitimately exceed the page.
		t.Logf("unread %d exceeds page size %d", all.Unread, len(all.Items))
	}
	if len(all.Items) == 0 {
		t.Skip("no derived notifications in this database")
	}
	// Newest first and every entry carries a machine type, never prose.
	for index, item := range all.Items {
		if index > 0 && item.OccurredAt.After(all.Items[index-1].OccurredAt) {
			t.Fatalf("items are not newest first at %d: %+v", index, all.Items)
		}
		switch item.Type {
		case "process_failed", "process_retry", "company_suspended", "subscription_past_due":
		default:
			t.Fatalf("unexpected type: %+v", item)
		}
		if item.Severity != "error" && item.Severity != "warning" {
			t.Fatalf("unexpected severity: %+v", item)
		}
		if !strings.HasPrefix(item.Key, "process:") && !strings.HasPrefix(item.Key, "company:") && !strings.HasPrefix(item.Key, "subscription:") {
			t.Fatalf("unexpected key: %+v", item)
		}
	}
	first := all.Items[0]
	if first.Read {
		t.Skip("first notification is already read in this database")
	}

	// Filters narrow the list without changing the unread total.
	process := list("?type=process")
	for _, item := range process.Items {
		if item.Type != "process_failed" && item.Type != "process_retry" {
			t.Fatalf("process filter leaked %s", item.Type)
		}
	}
	if process.Unread != all.Unread {
		t.Fatalf("unread changed with a filter: %d vs %d", process.Unread, all.Unread)
	}
	company := list("?type=company")
	for _, item := range company.Items {
		if item.Type != "company_suspended" {
			t.Fatalf("company filter leaked %s", item.Type)
		}
	}

	// Marking read is idempotent and scoped to this operator.
	body := `{"keys":["` + first.Key + `"]}`
	for i := 0; i < 2; i++ {
		if w := call("POST", "/api/v1/platform/notifications/read", body); w.Code != 204 {
			t.Fatalf("mark read: %d %s", w.Code, w.Body.String())
		}
	}
	afterRead := list("")
	if afterRead.Unread != all.Unread-1 {
		t.Fatalf("unread after read: %d want %d", afterRead.Unread, all.Unread-1)
	}
	unreadOnly := list("?read=unread")
	for _, item := range unreadOnly.Items {
		if item.Key == first.Key {
			t.Fatal("read notification still listed as unread")
		}
	}
	readOnly := list("?read=read")
	found := false
	for _, item := range readOnly.Items {
		if item.Key == first.Key {
			found = true
			if !item.Read {
				t.Fatalf("read filter returned an unread item: %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("read filter lacks the marked notification: %+v", readOnly.Items)
	}

	// Marking unread restores the badge.
	if w := call("POST", "/api/v1/platform/notifications/unread", body); w.Code != 204 {
		t.Fatalf("mark unread: %d %s", w.Code, w.Body.String())
	}
	if restored := list(""); restored.Unread != all.Unread {
		t.Fatalf("unread after clearing the marker: %d want %d", restored.Unread, all.Unread)
	}

	// Mark-all marks every current entry and is idempotent.
	for i := 0; i < 2; i++ {
		if w := call("POST", "/api/v1/platform/notifications/read-all", ""); w.Code != 204 {
			t.Fatalf("mark all: %d %s", w.Code, w.Body.String())
		}
	}
	if marked := list(""); marked.Unread != 0 {
		t.Fatalf("unread after mark-all: %d", marked.Unread)
	}
}

func TestNotificationValidation(t *testing.T) {
	_, _, call := notificationFixture(t)
	for _, query := range []string{"?type=bogus", "?read=bogus", "?limit=0", "?limit=51", "?limit=abc"} {
		if w := call("GET", "/api/v1/platform/notifications"+query, ""); w.Code != 400 {
			t.Fatalf("%s: %d want 400", query, w.Code)
		}
	}
	for _, body := range []string{
		`{}`,
		`{"keys":[]}`,
		`{"keys":["../../etc/passwd"]}`,
		`{"keys":["process:"]}`,
		`{"keys":["company:not-a-uuid!!"]}`,
		`{"keys":`,
	} {
		if w := call("POST", "/api/v1/platform/notifications/read", body); w.Code != 400 {
			t.Fatalf("%s: %d want 400", body, w.Code)
		}
	}
	// An unknown but well-formed key is accepted: the notification may have been
	// resolved between the read and the mark.
	if w := call("POST", "/api/v1/platform/notifications/read", `{"keys":["process:999999999"]}`); w.Code != 204 {
		t.Fatalf("well-formed unknown key: %d %s", w.Code, w.Body.String())
	}
	// Duplicate keys collapse instead of exceeding the per-request bound.
	dupes := make([]string, 0, 120)
	for i := 0; i < 120; i++ {
		dupes = append(dupes, `"process:999999999"`)
	}
	if w := call("POST", "/api/v1/platform/notifications/read", `{"keys":[`+strings.Join(dupes, ",")+`]}`); w.Code != 204 {
		t.Fatalf("duplicate keys: %d %s", w.Code, w.Body.String())
	}
}
