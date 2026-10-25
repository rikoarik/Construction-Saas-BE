package httpapi

import (
	"encoding/json"
	"net/http"
)

// NewRouter creates the minimal API router. Domain routes are added with their
// application-service dependencies as the corresponding backend tasks land.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	return mux
}
