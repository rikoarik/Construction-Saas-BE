package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// NewRouter creates the API router and optionally mounts the password-auth adapter.
func NewRouter(logger *slog.Logger, auth http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	if auth != nil {
		mux.Handle("/api/v1/auth/", auth)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, &Error{Status: http.StatusNotFound, Code: "NOT_FOUND"})
	})
	return RequestIDMiddleware(LoggingMiddleware(logger, mux))
}
