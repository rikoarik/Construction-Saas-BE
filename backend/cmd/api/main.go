package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/httpapi"
	"construction-saas/backend/internal/identity"
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
	authService := &identity.AuthService{Store: identityStore, Logger: logger}
	authMux := http.NewServeMux()
	identity.RegisterAuthRoutes(authMux, authService, os.Getenv("APP_ENV") == "production")

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	server := &http.Server{
		Addr: addr, Handler: httpapi.NewRouter(logger, authMux), ReadHeaderTimeout: 5 * time.Second,
	}
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
