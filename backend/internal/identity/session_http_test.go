package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionEndpointsRequireAuthentication(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAuthRoutes(mux, &AuthService{}, false)
	cases := []struct{ method, path string }{
		{"GET", "/api/v1/auth/me"}, {"GET", "/api/v1/auth/sessions"}, {"GET", "/api/v1/auth/companies"}, {"DELETE", "/api/v1/auth/sessions/123"},
	}
	for _, c := range cases {
		t.Run(c.method+c.path, func(t *testing.T) {
			res := httptest.NewRecorder()
			mux.ServeHTTP(res, httptest.NewRequest(c.method, c.path, nil))
			if res.Code != http.StatusUnauthorized {
				t.Fatalf("got %d", res.Code)
			}
		})
	}
}
