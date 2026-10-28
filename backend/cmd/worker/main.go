package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/email"
	"construction-saas/backend/internal/identity"
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
	identityStore, err := identity.NewStore(pool)
	if err != nil {
		logger.Error("create identity store", slog.String("error", err.Error()))
		os.Exit(1)
	}
	keyValue := os.Getenv("EMAIL_ENCRYPTION_KEY")
	if keyValue == "" {
		keyValue = os.Getenv("MFA_ENCRYPTION_KEY")
	}
	key, err := identity.ParseSecretKey(keyValue)
	if err != nil {
		logger.Error("EMAIL_ENCRYPTION_KEY or MFA_ENCRYPTION_KEY is required", slog.String("error", err.Error()))
		os.Exit(1)
	}
	box, err := identity.NewSecretBox(key)
	if err != nil {
		logger.Error("create email secret box", slog.String("error", err.Error()))
		os.Exit(1)
	}
	sender, err := newSMTPSender()
	if err != nil {
		logger.Error("configure SMTP", slog.String("error", err.Error()))
		os.Exit(1)
	}
	// Fail before claiming queued messages when the relay is unreachable.
	// Otherwise repeated startup attempts can exhaust the outbox retry budget.
	if err := sender.Ping(ctx); err != nil {
		logger.Error("SMTP relay unavailable; queued emails remain untouched", slog.String("error", err.Error()))
		os.Exit(1)
	}
	publicURL := os.Getenv("APP_PUBLIC_URL")
	if publicURL == "" {
		logger.Error("APP_PUBLIC_URL is required")
		os.Exit(1)
	}
	handler := &identity.EmailDeliveryHandler{
		Store: identityStore, Box: box, Sender: sender,
		PublicURL: publicURL, Logger: logger,
	}
	workerID := fmt.Sprintf("%.170s:%d", hostname(), os.Getpid())
	runner := outbox.Runner{
		Store: store, Handler: handler, Logger: logger,
		WorkerID: workerID, BatchSize: 25, Lease: time.Minute,
		PollInterval: time.Second, MaxAttempts: 8, BaseBackoff: time.Minute,
		EventTypes: []string{"email.delivery"},
		Heartbeat: func(ctx context.Context) error {
			return outbox.Heartbeat(ctx, pool, workerID, "email.delivery")
		},
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := outbox.RemoveHeartbeat(cleanupCtx, pool, workerID); err != nil {
			logger.Warn("remove worker heartbeat", slog.String("error", err.Error()))
		}
	}()
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("outbox worker stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("worker stopped")
}

func newSMTPSender() (*email.SMTPSender, error) {
	addr := os.Getenv("SMTP_ADDR")
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SMTP_TLS_MODE")))
	implicit := mode == "implicit" || mode == "smtps"
	if mode == "" {
		// Port 465 is SMTPS; every other port must upgrade with STARTTLS.
		implicit = strings.HasSuffix(addr, ":465")
	}
	if mode != "" && mode != "implicit" && mode != "smtps" && mode != "starttls" {
		return nil, errors.New("SMTP_TLS_MODE must be implicit, smtps, or starttls")
	}
	return email.NewSMTPSender(email.SMTPConfig{
		Addr:        addr,
		Host:        os.Getenv("SMTP_HOST"),
		Username:    os.Getenv("SMTP_USERNAME"),
		Password:    os.Getenv("SMTP_PASSWORD"),
		From:        os.Getenv("SMTP_FROM"),
		Timeout:     15 * time.Second,
		ImplicitTLS: implicit,
	})
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "worker"
	}
	return name
}
