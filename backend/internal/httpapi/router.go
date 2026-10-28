package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

// NewRouter creates the API router and optionally mounts the API handler.
func NewRouter(logger *slog.Logger, api http.Handler, security SecurityConfig) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	if api != nil {
		var apiRoutes http.Handler = api
		if security.LoginLimit != nil {
			limited := http.NewServeMux()
			limited.Handle("/api/v1/auth/login", security.LoginLimit.Middleware("login", api))
			limited.Handle("/", api)
			apiRoutes = limited
		}
		mux.Handle("/api/v1/", apiRoutes)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, &Error{Status: http.StatusNotFound, Code: "NOT_FOUND"})
	})
	var secured http.Handler = mux
	if security.CSRF.Enabled {
		secured = CSRFMiddleware(security.CSRF.SessionCookie, security.CSRF.Secret, security.CSRF.Resolver, security.CSRF.TrustedOrigins, secured)
	}
	if security.APILimiter != nil {
		secured = security.APILimiter.Middleware("api", secured)
	}
	secured = SecurityHeaders(security.HTTPS, secured)
	return RequestIDMiddleware(LoggingMiddleware(logger, secured))
}

type SecurityConfig struct {
	CSRF       CSRFConfig
	LoginLimit *RateLimiter
	APILimiter *RateLimiter
	HTTPS      bool
}

type CSRFConfig struct {
	Enabled        bool
	SessionCookie  string
	Secret         []byte
	Resolver       SessionResolver
	TrustedOrigins map[string]struct{}
}

func CSRFConfigFromEnv(resolver SessionResolver) (CSRFConfig, error) {
	secret := os.Getenv("CSRF_SECRET")
	if secret == "" {
		return CSRFConfig{}, nil
	}
	if len(secret) < 32 {
		return CSRFConfig{}, errors.New("CSRF_SECRET must contain at least 32 bytes")
	}
	origins := make(map[string]struct{})
	for _, origin := range strings.Split(os.Getenv("TRUSTED_ORIGINS"), ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins[origin] = struct{}{}
		}
	}
	return CSRFConfig{Enabled: true, SessionCookie: "session", Secret: []byte(secret), Resolver: resolver, TrustedOrigins: origins}, nil
}
