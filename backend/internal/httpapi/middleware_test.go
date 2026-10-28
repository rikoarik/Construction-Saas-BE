package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDMiddlewareAssignsFreshID(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := RequestID(r.Context()); len(id) != 32 {
			t.Errorf("context request ID = %q, want 32 hex characters", id)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	first := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "untrusted")
	handler.ServeHTTP(first, req)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if got := first.Header().Get("X-Request-ID"); len(got) != 32 || got == "untrusted" {
		t.Fatalf("first request ID = %q", got)
	}
	if first.Header().Get("X-Request-ID") == second.Header().Get("X-Request-ID") {
		t.Fatal("requests received the same request ID")
	}
}

func TestWriteErrorUsesStableEnvelope(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, "abc123"))
	w := httptest.NewRecorder()

	WriteError(w, r, &Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR", Params: map[string]any{"field": "name"}})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if got := w.Header().Get("X-Request-ID"); got != "abc123" {
		t.Fatalf("request ID = %q, want abc123", got)
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["code"] != "VALIDATION_ERROR" {
		t.Fatalf("code = %v, want VALIDATION_ERROR", payload["code"])
	}
	if strings.Contains(w.Body.String(), "internal") {
		t.Fatalf("unexpected internal details in body: %s", w.Body.String())
	}
}

func TestWriteErrorRedactsUnknownAndInvalidErrors(t *testing.T) {
	for _, err := range []error{
		errors.New("database password is secret"),
		&Error{Status: http.StatusOK, Code: "BROKEN"},
		&Error{Status: http.StatusBadRequest, Code: "not valid"},
	} {
		w := httptest.NewRecorder()
		WriteError(w, httptest.NewRequest(http.MethodGet, "/", nil), err)
		if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "INTERNAL_ERROR") {
			t.Errorf("response for %v = %d %s", err, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "BROKEN") {
			t.Errorf("response leaked internal error: %s", w.Body.String())
		}
	}
}

func TestLoggingMiddlewareRedactsURLAndLogsRequestID(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	handler := RequestIDMiddleware(LoggingMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/invitations/path-secret?token=query-secret", nil))

	if strings.Contains(output.String(), "path-secret") || strings.Contains(output.String(), "query-secret") {
		t.Fatalf("log leaked URL: %s", output.String())
	}
	if !strings.Contains(output.String(), w.Header().Get("X-Request-ID")) {
		t.Fatalf("log missing request ID: %s", output.String())
	}
	if !strings.Contains(output.String(), "status=204") {
		t.Fatalf("log missing status: %s", output.String())
	}
}

func TestRouterNotFoundUsesStableErrorEnvelope(t *testing.T) {
	res := httptest.NewRecorder()
	NewRouter(nil, nil, SecurityConfig{}).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/missing", nil))

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
	if got, want := res.Header().Get("Content-Type"), "application/json"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if got, want := res.Body.String(), "{\"code\":\"NOT_FOUND\"}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
