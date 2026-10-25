package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthz(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()

	NewRouter(logger, nil, SecurityConfig{}).ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if got, want := res.Header().Get("Content-Type"), "application/json"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if id := res.Header().Get("X-Request-ID"); len(id) != 32 {
		t.Fatalf("X-Request-ID = %q, want 32 hex characters", id)
	}
	if got, want := res.Body.String(), "{\"status\":\"ok\"}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if output.Len() == 0 {
		t.Fatal("router did not log request")
	}
}

func TestRouterSecurityMiddlewareIsComposed(t *testing.T) {
	resolver := sessionResolverFunc(func(*http.Request) (string, error) { return "session", nil })
	secret := []byte("0123456789abcdef0123456789abcdef")
	loginLimiter, err := NewRateLimiter(RateLimitConfig{Capacity: 1, Window: time.Minute, MaxKeys: 10})
	if err != nil {
		t.Fatal(err)
	}
	auth := http.NewServeMux()
	auth.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	security := SecurityConfig{
		CSRF:       CSRFConfig{Enabled: true, SessionCookie: "session", Secret: secret, Resolver: resolver, TrustedOrigins: map[string]struct{}{"https://app.example.test": {}}},
		LoginLimit: loginLimiter,
		HTTPS:      true,
	}
	handler := NewRouter(nil, auth, security)

	badOrigin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	badOrigin.Header.Set("Origin", "https://evil.example.test")
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, badOrigin)
	if first.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login = %d, want 403", first.Code)
	}
	secondReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	secondReq.Header.Set("Origin", "https://app.example.test")
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, secondReq)
	if second.Code != http.StatusNoContent {
		t.Fatalf("same-origin login = %d, want 204", second.Code)
	}
	thirdReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	thirdReq.Header.Set("Origin", "https://app.example.test")
	third := httptest.NewRecorder()
	handler.ServeHTTP(third, thirdReq)
	if third.Code != http.StatusTooManyRequests || !strings.Contains(third.Body.String(), "RATE_LIMITED") {
		t.Fatalf("rate limited login = %d %s", third.Code, third.Body.String())
	}
	if third.Header().Get("X-Content-Type-Options") != "nosniff" || third.Header().Get("Strict-Transport-Security") == "" {
		t.Fatalf("security headers missing from throttled response: %v", third.Header())
	}
}
