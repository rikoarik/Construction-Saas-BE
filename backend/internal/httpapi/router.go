package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// NewRouter creates the minimal API router. Domain routes are added with their
// application-service dependencies as the corresponding backend tasks land.
func NewRouter(logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, &Error{Status: http.StatusNotFound, Code: "NOT_FOUND"})
	})
	return RequestIDMiddleware(LoggingMiddleware(logger, mux))
}
