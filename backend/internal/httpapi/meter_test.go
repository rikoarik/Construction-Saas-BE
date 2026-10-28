package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type countingMeter struct{ calls int }

func (m *countingMeter) Record() { m.calls++ }

func TestMeteringMiddlewareCountsEveryRequest(t *testing.T) {
	meter := &countingMeter{}
	handler := MeteringMiddleware(meter, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for range 3 {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/platform/me", nil))
	}
	if meter.calls != 3 {
		t.Fatalf("recorded %d requests, want 3", meter.calls)
	}
}

func TestMeteringMiddlewareWithoutMeterPassesThrough(t *testing.T) {
	served := false
	handler := MeteringMiddleware(nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true }))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if !served {
		t.Fatal("request was not forwarded")
	}
}
