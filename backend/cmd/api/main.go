package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/httpapi"
	"construction-saas/backend/internal/identity"
	"construction-saas/backend/internal/tenancy"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		logger.Error("open PostgreSQL", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, databaseURL); err != nil {
		logger.Error("migrate PostgreSQL", slog.String("error", err.Error()))
		os.Exit(1)
	}
	identityStore, err := identity.NewStore(pool)
	if err != nil {
		logger.Error("create identity store", slog.String("error", err.Error()))
		os.Exit(1)
	}
	tenancyStore, err := tenancy.NewStore(pool)
	if err != nil {
		logger.Error("create tenancy store", slog.String("error", err.Error()))
		os.Exit(1)
	}
	authService := &identity.AuthService{Store: identityStore, Logger: logger}
	authMux := http.NewServeMux()
	identity.RegisterAuthRoutes(authMux, authService, os.Getenv("APP_ENV") == "production")
	(&tenancy.Handler{Store: tenancyStore, Auth: authService}).Register(authMux)

	csrfConfig, err := securityCSRFConfig(authService)
	if err != nil {
		logger.Error("configure CSRF", slog.String("error", err.Error()))
		os.Exit(1)
	}
	loginLimiter, err := rateLimiterFromEnv("LOGIN_RATE_LIMIT", httpapi.RateLimitConfig{Capacity: 10, Window: time.Minute, MaxKeys: 10000})
	if err != nil {
		logger.Error("configure login rate limit", slog.String("error", err.Error()))
		os.Exit(1)
	}
	apiLimiter, err := rateLimiterFromEnv("API_RATE_LIMIT", httpapi.RateLimitConfig{Capacity: 120, Window: time.Minute, MaxKeys: 10000})
	if err != nil {
		logger.Error("configure API rate limit", slog.String("error", err.Error()))
		os.Exit(1)
	}
	security := httpapi.SecurityConfig{CSRF: csrfConfig, LoginLimit: loginLimiter, APILimiter: apiLimiter, HTTPS: os.Getenv("APP_ENV") == "production"}

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	server := &http.Server{Addr: addr, Handler: httpapi.NewRouter(logger, authMux, security), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("server shutdown", slog.String("error", err.Error()))
		}
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

type authSessionResolver struct{ service *identity.AuthService }

func (r authSessionResolver) ResolveSessionID(request *http.Request) (string, error) {
	cookie, err := request.Cookie(identity.SessionCookieName)
	if err != nil {
		return "", identity.ErrUnauthenticated
	}
	session, err := r.service.Authenticate(request.Context(), cookie.Value)
	if err != nil {
		return "", err
	}
	return session.ID, nil
}

func securityCSRFConfig(service *identity.AuthService) (httpapi.CSRFConfig, error) {
	secret := os.Getenv("CSRF_SECRET")
	if secret == "" {
		if os.Getenv("APP_ENV") == "production" {
			return httpapi.CSRFConfig{}, errors.New("CSRF_SECRET is required in production")
		}
		return httpapi.CSRFConfig{}, nil
	}
	if len(secret) < 32 {
		return httpapi.CSRFConfig{}, errors.New("CSRF_SECRET must contain at least 32 bytes")
	}
	origins := make(map[string]struct{})
	for _, origin := range strings.Split(os.Getenv("TRUSTED_ORIGINS"), ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins[origin] = struct{}{}
		}
	}
	return httpapi.CSRFConfig{Enabled: true, SessionCookie: identity.SessionCookieName, Secret: []byte(secret), Resolver: authSessionResolver{service: service}, TrustedOrigins: origins}, nil
}

func rateLimiterFromEnv(prefix string, defaults httpapi.RateLimitConfig) (*httpapi.RateLimiter, error) {
	capacity, window, maxKeys := defaults.Capacity, defaults.Window, defaults.MaxKeys
	if value := os.Getenv(prefix + "_CAPACITY"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return nil, err
		}
		capacity = parsed
	}
	if value := os.Getenv(prefix + "_WINDOW"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return nil, err
		}
		window = parsed
	}
	if value := os.Getenv(prefix + "_MAX_KEYS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return nil, err
		}
		maxKeys = parsed
	}
	return httpapi.NewRateLimiter(httpapi.RateLimitConfig{Capacity: capacity, Window: window, MaxKeys: maxKeys})
}
