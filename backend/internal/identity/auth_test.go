package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthRoutesRejectMalformedPayloads(t *testing.T) {
	service := &AuthService{}
	mux := http.NewServeMux()
	RegisterAuthRoutes(mux, service, false)
	for _, body := range []string{"{", `{"email":"a@example.com","password":"x","extra":true}`, `{"email":"a@example.com","password":"x"} {}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest {
			t.Errorf("malformed payload status = %d, want 400", res.Code)
		}
	}
}

func TestAuthIntegrationLoginCookieLogoutAndRevocation(t *testing.T) {
	store, user := integrationIdentity(t)
	hasher := PasswordHasher{}
	password := "correct horse battery staple"
	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(context.Background(), `UPDATE app.users SET password_hash=$2 WHERE id=$1::uuid`, user.ID, encoded); err != nil {
		t.Fatal(err)
	}
	service := &AuthService{Store: store, SessionTTL: time.Hour}
	mux := http.NewServeMux()
	RegisterAuthRoutes(mux, service, true)

	login := func(email, pass string) *httptest.ResponseRecorder {
		reqBody, _ := json.Marshal(Credentials{Email: email, Password: pass})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(string(reqBody)))
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		return res
	}
	badPassword := login(user.Email, "wrong password")
	unknownUser := login("unknown@example.invalid", "wrong password")
	if badPassword.Code != http.StatusUnauthorized || unknownUser.Code != http.StatusUnauthorized || badPassword.Body.String() != unknownUser.Body.String() {
		t.Fatalf("login errors differ: bad=%d %q unknown=%d %q", badPassword.Code, badPassword.Body.String(), unknownUser.Code, unknownUser.Body.String())
	}
	response := login(strings.ToUpper(user.Email), password)
	if response.Code != http.StatusNoContent {
		t.Fatalf("login status = %d body=%s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want one", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != SessionCookieName || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Value == "" {
		t.Fatalf("login cookie attributes = %+v", cookie)
	}
	if _, err := store.GetActiveSessionByTokenHash(context.Background(), sessionTokenHash(cookie.Value)); err != nil {
		t.Fatalf("persisted cookie session: %v", err)
	}
	if _, err := store.GetActiveSessionByTokenHash(context.Background(), cookie.Value); !errors.Is(err, ErrNotFound) {
		t.Fatalf("raw cookie token lookup = %v, want not found", err)
	}
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.AddCookie(cookie)
	logoutRes := httptest.NewRecorder()
	mux.ServeHTTP(logoutRes, logoutReq)
	if logoutRes.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d body=%s", logoutRes.Code, logoutRes.Body.String())
	}
	clearedCookies := logoutRes.Result().Cookies()
	if len(clearedCookies) != 1 || clearedCookies[0].MaxAge >= 0 || clearedCookies[0].Value != "" {
		t.Fatalf("logout did not clear cookie: %+v", clearedCookies)
	}
	if _, err := store.GetActiveSessionByTokenHash(context.Background(), sessionTokenHash(cookie.Value)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("logged out token lookup = %v, want not found", err)
	}
	if err := service.Logout(context.Background(), user.ID, cookie.Value); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("replayed logout = %v, want ErrUnauthenticated", err)
	}
}

func integrationIdentity(t *testing.T) (*Store, User) {
	t.Helper()
	pool := integrationPool(t)
	store, err := NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	email := "auth-test-" + time.Now().Format("20060102150405.000000000") + "@example.invalid"
	user, err := store.CreateUser(context.Background(), email, "$placeholder$", "active")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM app.auth_sessions WHERE user_id=$1::uuid`, user.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM app.users WHERE id=$1::uuid`, user.ID)
	})
	return store, user
}

func TestExpiredSessionsAreNotAuthenticated(t *testing.T) {
	store, user := integrationIdentity(t)
	tokenHash := "expired-hash-" + time.Now().Format("20060102150405.000000000")
	if _, err := store.pool.Exec(context.Background(), `INSERT INTO app.auth_sessions(user_id,token_hash,created_at,expires_at) VALUES ($1::uuid,$2,now()-interval '2 hours',now()-interval '1 hour')`, user.ID, tokenHash); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetActiveSessionByTokenHash(context.Background(), tokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session lookup = %v, want ErrNotFound", err)
	}
}
