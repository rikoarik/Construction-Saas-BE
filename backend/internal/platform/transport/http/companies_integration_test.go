package platformhttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/email"
	"construction-saas/backend/internal/httpapi"
	"construction-saas/backend/internal/identity"
	"construction-saas/backend/internal/outbox"
	"construction-saas/backend/internal/platform/app"
	platformpg "construction-saas/backend/internal/platform/infra/postgres"
)

type captureInvitationSender struct{ messages []email.Message }

func (s *captureInvitationSender) Send(_ context.Context, m email.Message) error {
	s.messages = append(s.messages, m)
	return nil
}

func TestCompanyModuleIntegration(t *testing.T) {
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
	user, err := store.CreateUser(ctx, fmt.Sprintf("operator-%d@example.invalid", time.Now().UnixNano()), "hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, fmt.Sprintf("other-%d@example.invalid", time.Now().UnixNano()), "hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO app.platform_role_assignments(user_id,role_code) VALUES($1::uuid,'superadmin')`, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := fmt.Sprintf("company-module-session-%d", time.Now().UnixNano())
	digest := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(digest[:])
	_, err = store.CreateSession(ctx, user.ID, hash, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE app.auth_sessions SET mfa_verified_at=now() WHERE token_hash=$1`, hash)
	if err != nil {
		t.Fatal(err)
	}
	auth := &identity.AuthService{Store: store, EmailKey: []byte("0123456789abcdef0123456789abcdef"), PublicURL: "https://example.invalid"}
	mux := http.NewServeMux()
	Handler{Auth: auth, Companies: app.CompanyManagement{Repository: platformpg.Companies{Pool: pool, Auth: auth}}}.Register(mux)
	handler := httpapi.NewRouter(nil, mux, httpapi.SecurityConfig{})
	request := func(method, path, body, key string, authenticated bool) (int, []byte) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "session", Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	code := fmt.Sprintf("module_%d", time.Now().UnixNano())
	input := fmt.Sprintf(`{"code":%q,"name":"Alpha Build","contact_email":"alpha@example.invalid","admin_email":%q}`, code, fmt.Sprintf("first-%s@example.invalid", code))
	if status, _ := request("POST", "/api/v1/platform/companies", input, "first", false); status != 401 {
		t.Fatalf("unauthenticated: %d", status)
	}
	status, raw := request("POST", "/api/v1/platform/companies", input, "first", true)
	if status != 201 {
		t.Fatalf("create %d: %s", status, raw)
	}
	var company app.CompanyProfile
	if err = json.Unmarshal(raw, &company); err != nil {
		t.Fatal(err)
	}
	if company.Status != "provisioning" || company.Code != code {
		t.Fatalf("company: %+v", company)
	}
	status, retry := request("POST", "/api/v1/platform/companies", input, "first", true)
	if status != 201 || string(raw) != string(retry) {
		t.Fatalf("idempotent retry %d %s", status, retry)
	}
	status, _ = request("POST", "/api/v1/platform/companies", strings.Replace(input, "Alpha Build", "Different", 1), "first", true)
	if status != 409 {
		t.Fatalf("idempotency conflict: %d", status)
	}
	status, _ = request("POST", "/api/v1/platform/companies", input, "second", true)
	if status != 409 {
		t.Fatalf("duplicate code: %d", status)
	}
	root := "/api/v1/platform/companies/" + company.ID
	for _, path := range []string{"/api/v1/platform/companies?search=" + code + "&status=provisioning&limit=1", root, root + "/usage", root + "/members"} {
		status, raw = request("GET", path, "", "", true)
		if status != 200 {
			t.Fatalf("get %s: %d %s", path, status, raw)
		}
	}
	status, raw = request("GET", "/api/v1/platform/companies?search="+code+"&status=active", "", "", true)
	if status != 200 || strings.Contains(string(raw), company.ID) {
		t.Fatalf("filter: %d %s", status, raw)
	}
	for _, query := range []string{"search=" + code + "&sort=name&limit=1&offset=0", "search=alpha@example.invalid&status=provisioning", "search=Alpha%20Build&sort=-code"} {
		status, raw = request("GET", "/api/v1/platform/companies?"+query, "", "", true)
		if status != 200 || !strings.Contains(string(raw), company.ID) {
			t.Fatalf("company search %s: %d %s", query, status, raw)
		}
	}
	status, _ = request("GET", "/api/v1/platform/companies?limit=101", "", "", true)
	if status != 400 {
		t.Fatalf("page validation: %d", status)
	}
	status, raw = request("PATCH", root, `{"name":"Alpha Revised"}`, "", true)
	if status != 200 || !strings.Contains(string(raw), "Alpha Revised") {
		t.Fatalf("update: %d %s", status, raw)
	}
	var invitationID string
	err = pool.QueryRow(ctx, `SELECT id::text FROM app.company_invitations WHERE company_id=$1::uuid`, company.ID).Scan(&invitationID)
	if err != nil {
		t.Fatal(err)
	}
	var queued int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM app.outbox_events WHERE company_id=$1::uuid AND event_type='email.delivery'`, company.ID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("outbox after retry: %d %v", queued, err)
	}
	status, raw = request("POST", root+"/invitations/"+invitationID+"/resend", "", "", true)
	if status != 200 {
		t.Fatalf("provisioning invite resend %d %s", status, raw)
	}
	var firstDeliveryID string
	if err = pool.QueryRow(ctx, `SELECT delivery_id::text FROM app.company_invitations WHERE id=$1::uuid`, invitationID).Scan(&firstDeliveryID); err != nil {
		t.Fatal(err)
	}
	// An obsolete queued event must not send a revoked invitation.
	oldSender := &captureInvitationSender{}
	boxBefore, err := identity.NewSecretBox(auth.EmailKey)
	if err != nil {
		t.Fatal(err)
	}
	oldHandler := &identity.EmailDeliveryHandler{Store: store, Box: boxBefore, Sender: oldSender, PublicURL: auth.PublicURL}
	oldPayload, _ := json.Marshal(map[string]string{"delivery_id": firstDeliveryID})
	if err = oldHandler.Handle(ctx, outbox.Event{Type: "email.delivery", Payload: oldPayload, CompanyID: &company.ID}); err != nil || len(oldSender.messages) != 0 {
		t.Fatalf("revoked invite delivered: %v %d", err, len(oldSender.messages))
	}
	// Accept the first invitation through the existing identity service.
	var firstInvite app.AdminInvitation
	if err = json.Unmarshal(raw, &firstInvite); err != nil {
		t.Fatal(err)
	}
	var cipher, nonce []byte
	if err = pool.QueryRow(ctx, `SELECT d.token_ciphertext,d.token_nonce FROM app.company_invitations i JOIN app.auth_email_deliveries d ON d.id=i.delivery_id WHERE i.id=$1::uuid`, firstInvite.ID).Scan(&cipher, &nonce); err != nil {
		t.Fatal(err)
	}
	box, err := identity.NewSecretBox(auth.EmailKey)
	if err != nil {
		t.Fatal(err)
	}
	var currentDeliveryID string
	if err = pool.QueryRow(ctx, `SELECT delivery_id::text FROM app.company_invitations WHERE id=$1::uuid`, firstInvite.ID).Scan(&currentDeliveryID); err != nil {
		t.Fatal(err)
	}
	activePayload, _ := json.Marshal(map[string]string{"delivery_id": currentDeliveryID})
	if err = oldHandler.Handle(ctx, outbox.Event{Type: "email.delivery", Payload: activePayload, CompanyID: &company.ID}); err != nil || len(oldSender.messages) != 1 {
		t.Fatalf("active invite email: %v %d", err, len(oldSender.messages))
	}
	secret, err := box.Decrypt(cipher, nonce, "auth-email-v1:company_invitation:")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := auth.AcceptInvitation(ctx, string(secret), "StrongTestingPassword#2026", nil, identity.ClientInfo{})
	if err != nil {
		t.Fatalf("first admin acceptance: %v", err)
	}
	if accepted.ID == "" {
		t.Fatal("missing account")
	}
	if _, err = auth.AcceptInvitation(ctx, string(secret), "StrongTestingPassword#2026", nil, identity.ClientInfo{}); err != identity.ErrInvitationInvalid {
		t.Fatalf("token replay: %v", err)
	}
	activated, err := store.TenantRole(ctx, accepted.ID, company.ID)
	if err != nil || activated != "admin" {
		t.Fatalf("provisioning activation: %s %v", activated, err)
	}
	invited := accepted
	if _, err = store.TenantRole(ctx, other.ID, company.ID); err != identity.ErrCompanyAccess {
		t.Fatalf("cross-tenant: %v", err)
	}
	if _, err = store.TenantRole(ctx, user.ID, company.ID); err != identity.ErrCompanyAccess {
		t.Fatalf("platform role granted tenant access: %v", err)
	}
	var secondCompany string
	if err = pool.QueryRow(ctx, `INSERT INTO app.companies(name,slug,code,status) VALUES('Other Workspace',$1,$1,'active') RETURNING id::text`, "second_"+code).Scan(&secondCompany); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO app.company_memberships(company_id,user_id,role,status) VALUES($1::uuid,$2::uuid,'admin','active')`, secondCompany, invited.ID); err != nil {
		t.Fatal(err)
	}
	if role, err := store.TenantRole(ctx, invited.ID, secondCompany); err != nil || role != "admin" {
		t.Fatalf("multi-company membership: %s %v", role, err)
	}
	status, _ = request("PATCH", root+"/members/"+invited.ID, `{"status":"suspended"}`, "", true)
	if status != 409 {
		t.Fatalf("last admin: %d", status)
	}
	status, raw = request("POST", root+"/suspend", `{"reason":"account review"}`, "", true)
	if status != 200 {
		t.Fatalf("suspend %d %s", status, raw)
	}
	if _, err = store.TenantRole(ctx, invited.ID, company.ID); err != identity.ErrCompanyAccess {
		t.Fatalf("suspension not immediate: %v", err)
	}
	if role, err := store.TenantRole(ctx, invited.ID, secondCompany); err != nil || role != "admin" {
		t.Fatalf("suspension crossed companies: %s %v", role, err)
	}
	status, _ = request("POST", root+"/suspend", `{"reason":"again"}`, "", true)
	if status != 409 {
		t.Fatalf("status transition: %d", status)
	}
	status, raw = request("POST", root+"/reactivate", "", "", true)
	if status != 200 {
		t.Fatalf("reactivate %d %s", status, raw)
	}
	if _, err = store.TenantRole(ctx, invited.ID, company.ID); err != nil {
		t.Fatalf("reactivation: %v", err)
	}
	status, raw = request("POST", root+"/invitations", `{"email":"second@example.invalid"}`, "", true)
	if status != 201 {
		t.Fatalf("invite %d %s", status, raw)
	}
	var invite app.AdminInvitation
	_ = json.Unmarshal(raw, &invite)
	status, _ = request("POST", root+"/invitations", `{"email":"second@example.invalid"}`, "", true)
	if status != 409 {
		t.Fatalf("duplicate invite: %d", status)
	}
	status, raw = request("POST", root+"/invitations/"+invite.ID+"/resend", "", "", true)
	if status != 200 {
		t.Fatalf("resend %d %s", status, raw)
	}
	var resent app.AdminInvitation
	_ = json.Unmarshal(raw, &resent)
	status, _ = request("DELETE", root+"/invitations/"+resent.ID, "", "", true)
	if status != 204 {
		t.Fatalf("cancel: %d", status)
	}
	// Concurrent deactivations serialize on the company row.
	second, err := store.CreateUser(ctx, fmt.Sprintf("second-%d@example.invalid", time.Now().UnixNano()), "hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO app.company_memberships(company_id,user_id,role,status) VALUES($1::uuid,$2::uuid,'admin','active')`, company.ID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for _, id := range []string{invited.ID, second.ID} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			status, _ := request("PATCH", root+"/members/"+id, `{"status":"suspended"}`, "", true)
			results <- status
		}(id)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for result := range results {
		if result == 200 {
			success++
		}
		if result == 409 {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent deactivation: %d success, %d conflict", success, conflict)
	}
	var active int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM app.company_memberships WHERE company_id=$1::uuid AND status='active' AND role IN ('owner','admin')`, company.ID).Scan(&active)
	if active != 1 {
		t.Fatalf("active admins: %d", active)
	}
}
