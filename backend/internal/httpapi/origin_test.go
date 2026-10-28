package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllAPIMutationsRequireTrustedOrigin(t *testing.T) {
	handler := CSRFMiddleware("session", []byte("0123456789abcdef0123456789abcdef"), sessionResolverFunc(func(*http.Request) (string, error) { return "sid", nil }), map[string]struct{}{"https://app.example.test": {}}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, path := range []string{"/api/v1/auth/forgot-password", "/api/v1/auth/reset-password", "/api/v1/auth/invitations/accept", "/api/v1/auth/refresh"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.Header.Set("Origin", "https://evil.example.test")
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != http.StatusForbidden {
				t.Fatalf("status=%d", res.Code)
			}
		})
	}
}
