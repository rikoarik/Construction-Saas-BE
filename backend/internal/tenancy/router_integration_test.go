package tenancy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"construction-saas/backend/internal/httpapi"
	"construction-saas/backend/internal/identity"
)

func TestCompanyRoutesReachAuthenticationThroughRouter(t *testing.T) {
	api := http.NewServeMux()
	(&Handler{Store: &Store{}, Auth: &identity.AuthService{}}).Register(api)
	router := httpapi.NewRouter(nil, api, httpapi.SecurityConfig{})

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/companies"},
		{http.MethodGet, "/api/v1/companies"},
		{http.MethodGet, "/api/v1/companies/example/members"},
		{http.MethodPost, "/api/v1/companies/example/invitations"},
		{http.MethodGet, "/api/v1/companies/example/invitations"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "UNAUTHENTICATED") {
				t.Fatalf("got %d %s; want 401 UNAUTHENTICATED", response.Code, response.Body.String())
			}
		})
	}
}
