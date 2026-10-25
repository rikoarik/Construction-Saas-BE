package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCSRFMiddlewareRejectsMissingAndWrongTokenAndAllowsMatchingToken(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	resolver := sessionResolverFunc(func(*http.Request) (string, error) { return "session-1", nil })
	called := 0
	handler := CSRFMiddleware("session", secret, resolver, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, token := range []string{"", "invalid"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/resource", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
		if token != "" {
			req.Header.Set(CSRFHeader, token)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusForbidden {
			t.Errorf("invalid token status = %d, want 403", res.Code)
		}
	}
	if called != 0 {
		t.Fatalf("handler called %d times on CSRF rejection", called)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/resource", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
	req.Header.Set(CSRFHeader, csrfValue(secret, "session-1"))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("valid CSRF response = %d, handler calls=%d", res.Code, called)
	}
}

func TestCSRFMiddlewareProtectsLogoutAndChecksLoginOrigin(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	resolver := sessionResolverFunc(func(*http.Request) (string, error) { return "session-1", nil })
	called := 0
	handler := CSRFMiddleware("session", secret, resolver, map[string]struct{}{"https://app.example.test": {}}, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}))
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	login.Header.Set("Origin", "https://evil.example.test")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, login)
	if res.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login status = %d, want 403", res.Code)
	}
	login.Header.Set("Origin", "https://app.example.test")
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, login)
	if res.Code != http.StatusNoContent {
		t.Fatalf("same-origin login status = %d, want 204", res.Code)
	}
	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, logout)
	if res.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF status = %d, want 403", res.Code)
	}
	if called != 1 {
		t.Fatalf("handler calls = %d, want one same-origin login", called)
	}
}

func TestCSRFTokenBootstrapIsNoStoreAndBoundToSession(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	resolver := sessionResolverFunc(func(*http.Request) (string, error) { return "session-a", nil })
	handler := CSRFMiddleware("session", secret, resolver, nil, http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodGet, CSRFTokenPath, nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "opaque"})
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Header().Get("Cache-Control") != "no-store" || !strings.Contains(res.Body.String(), csrfValue(secret, "session-a")) {
		t.Fatalf("bootstrap response = %d %v %s", res.Code, res.Header(), res.Body.String())
	}
	if validCSRFToken(secret, "session-b", csrfValue(secret, "session-a")) {
		t.Fatal("token validated for another session")
	}
}

func TestRateLimiterEnforcesConfiguredCapacityAndIgnoresForwardingHeaders(t *testing.T) {
	limiter, err := NewRateLimiter(RateLimitConfig{Capacity: 1, Window: time.Minute, MaxKeys: 10})
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	handler := limiter.Middleware("login", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}))
	for index := 0; index < 2; index++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "192.0.2.1:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.99")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if index == 0 && res.Code != http.StatusNoContent {
			t.Fatalf("first response = %d, want 204", res.Code)
		}
		if index == 1 && (res.Code != http.StatusTooManyRequests || res.Header().Get("Retry-After") == "" || !strings.Contains(res.Body.String(), "RATE_LIMITED")) {
			t.Fatalf("second response = %d headers=%v body=%s", res.Code, res.Header(), res.Body.String())
		}
	}
	if called != 1 {
		t.Fatalf("downstream called %d times, want 1", called)
	}
}

func TestRateLimiterRejectsNewKeysWhenBoundIsFull(t *testing.T) {
	limiter, err := NewRateLimiter(RateLimitConfig{Capacity: 2, Window: time.Minute, MaxKeys: 1})
	if err != nil {
		t.Fatal(err)
	}
	if allowed, _ := limiter.Allow("first"); !allowed {
		t.Fatal("first key denied")
	}
	if allowed, _ := limiter.Allow("second"); allowed {
		t.Fatal("new key exceeded configured cardinality bound")
	}
}

func TestSecurityHeadersCoverErrorResponsesAndHSTSOptIn(t *testing.T) {
	handler := SecurityHeaders(true, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, httptest.NewRequest(http.MethodGet, "/", nil), &Error{Status: http.StatusNotFound, Code: "NOT_FOUND"})
	}))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if res.Code != http.StatusNotFound || res.Header().Get("X-Content-Type-Options") != "nosniff" || res.Header().Get("X-Frame-Options") != "DENY" || res.Header().Get("Strict-Transport-Security") == "" {
		t.Fatalf("error response headers/status = %d %v", res.Code, res.Header())
	}
	plain := httptest.NewRecorder()
	SecurityHeaders(false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/", nil))
	if plain.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS emitted without HTTPS deployment flag")
	}
}
