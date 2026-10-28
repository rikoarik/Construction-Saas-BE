package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompanyAndSessionIsolation(t *testing.T) {
	store, user := integrationIdentity(t)
	_, other := integrationIdentity(t)
	ctx := context.Background()
	var active, suspended string
	err := store.pool.QueryRow(ctx, `INSERT INTO app.companies (name,slug,status) VALUES ('Active', $1, 'active') RETURNING id::text`, "active-"+user.ID).Scan(&active)
	if err != nil {
		t.Fatal(err)
	}
	err = store.pool.QueryRow(ctx, `INSERT INTO app.companies (name,slug,status) VALUES ('Suspended', $1, 'suspended') RETURNING id::text`, "suspended-"+user.ID).Scan(&suspended)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `UPDATE app.auth_sessions SET selected_company_id=NULL WHERE selected_company_id IN ($1::uuid,$2::uuid)`, active, suspended)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM app.company_memberships WHERE company_id IN ($1::uuid,$2::uuid)`, active, suspended)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM app.companies WHERE id IN ($1::uuid,$2::uuid)`, active, suspended)
	})
	for _, id := range []string{active, suspended} {
		if _, err = store.pool.Exec(ctx, `INSERT INTO app.company_memberships(company_id,user_id,role) VALUES($1::uuid,$2::uuid,'member')`, id, user.ID); err != nil {
			t.Fatal(err)
		}
	}
	own, err := store.CreateSession(ctx, user.ID, sessionTokenHash("own-token"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := store.CreateSession(ctx, other.ID, sessionTokenHash("other-token"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterAuthRoutes(mux, &AuthService{Store: store}, false)
	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "own-token"})
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		return res
	}
	res := request("GET", "/api/v1/auth/companies")
	if res.Code != 200 {
		t.Fatalf("companies status %d", res.Code)
	}
	var body struct {
		Companies []CompanyAccess `json:"companies"`
	}
	if err = json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Companies) != 1 || body.Companies[0].ID != active {
		t.Fatalf("unexpected companies %+v", body.Companies)
	}
	if err := store.SelectCompany(ctx, own, suspended); !errors.Is(err, ErrCompanyAccess) {
		t.Fatalf("suspended company: %v", err)
	}
	if err := store.SelectCompany(ctx, foreign, active); !errors.Is(err, ErrCompanyAccess) {
		t.Fatalf("cross tenant: %v", err)
	}
	if err := store.SelectCompany(ctx, own, active); err != nil {
		t.Fatal(err)
	}
	selected, err := store.SelectedCompany(ctx, own)
	if err != nil || selected == nil || selected.ID != active {
		t.Fatalf("selected: %+v %v", selected, err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE app.companies SET status='suspended' WHERE id=$1::uuid`, active); err != nil {
		t.Fatal(err)
	}
	selected, err = store.SelectedCompany(ctx, own)
	if err != nil || selected != nil {
		t.Fatalf("suspended selection: %+v %v", selected, err)
	}
	res = request("DELETE", "/api/v1/auth/sessions/"+foreign.ID)
	if res.Code != 404 {
		t.Fatalf("foreign session status %d", res.Code)
	}
	if _, err = store.GetActiveSessionByTokenHash(ctx, sessionTokenHash("other-token")); err != nil {
		t.Fatal(err)
	}
	res = request("DELETE", "/api/v1/auth/sessions/"+own.ID)
	if res.Code != 204 {
		t.Fatalf("own session status %d", res.Code)
	}
	if _, err = store.GetActiveSessionByTokenHash(ctx, sessionTokenHash("own-token")); err == nil {
		t.Fatal("session remains active")
	}
}
