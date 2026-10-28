package identity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLogoutAllRejectsAnonymous(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAuthRoutes(mux, &AuthService{}, false)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout-all", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", res.Code)
	}
}

func TestLogoutAllRevokesOnlyOwnSessions(t *testing.T) {
	store, user := integrationIdentity(t)
	_, other := integrationIdentity(t)
	ctx := context.Background()
	for _, item := range []struct{ user, token string }{{user.ID, "token-a"}, {user.ID, "token-b"}, {other.ID, "token-c"}} {
		if _, err := store.CreateSession(ctx, item.user, sessionTokenHash(item.token), time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	RegisterAuthRoutes(mux, &AuthService{Store: store}, false)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout-all", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "token-a"})
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("got %d", res.Code)
	}
	for _, token := range []string{"token-a", "token-b"} {
		if _, err := store.GetActiveSessionByTokenHash(ctx, sessionTokenHash(token)); err == nil {
			t.Fatalf("%s remains active", token)
		}
	}
	if _, err := store.GetActiveSessionByTokenHash(ctx, sessionTokenHash("token-c")); err != nil {
		t.Fatalf("other session: %v", err)
	}
}
