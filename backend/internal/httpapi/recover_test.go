package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecoverDoesNotExposePanic(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	handler := NewRouter(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("secret-panic-value")
	}), SecurityConfig{})
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/test", nil))
	if res.Code != http.StatusInternalServerError || !strings.Contains(res.Body.String(), "INTERNAL_ERROR") {
		t.Fatalf("unexpected response: %d %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "secret-panic-value") || strings.Contains(logs.String(), "secret-panic-value") {
		t.Fatal("panic value leaked")
	}
}
