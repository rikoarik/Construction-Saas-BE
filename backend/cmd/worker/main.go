package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/outbox"
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
	store, err := outbox.NewStore(pool)
	if err != nil {
		logger.Error("create outbox store", slog.String("error", err.Error()))
		os.Exit(1)
	}
	runner := outbox.Runner{
		Store: store, Handler: loggingHandler{logger: logger}, Logger: logger,
		WorkerID: hostname(), BatchSize: 25, Lease: time.Minute,
		PollInterval: time.Second, MaxAttempts: 8, BaseBackoff: time.Second,
	}
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("outbox worker stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("worker stopped")
}

type loggingHandler struct{ logger *slog.Logger }

func (h loggingHandler) Handle(ctx context.Context, event outbox.Event) error {
	h.logger.InfoContext(ctx, "outbox event has no registered handler",
		slog.Int64("event_id", event.ID), slog.String("event_type", event.Type))
	return errors.New("no handler registered for outbox event type")
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "worker"
	}
	return name
}
