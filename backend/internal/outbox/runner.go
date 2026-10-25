package outbox

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// Handler performs one event's provider-neutral side effect. Implementations
// must be idempotent by event ID or idempotency key because leases can expire.
type Handler interface {
	Handle(context.Context, Event) error
}

// Runner polls the PostgreSQL outbox and processes events with bounded batches.
type Runner struct {
	Store        *Store
	Handler      Handler
	Logger       *slog.Logger
	WorkerID     string
	BatchSize    int
	Lease        time.Duration
	PollInterval time.Duration
	MaxAttempts  int
	BaseBackoff  time.Duration
}

func (r *Runner) Run(ctx context.Context) error {
	if r.Store == nil || r.Handler == nil || r.WorkerID == "" || r.BatchSize < 1 || r.Lease <= 0 || r.PollInterval <= 0 || r.MaxAttempts < 1 || r.BaseBackoff <= 0 {
		return errors.New("outbox runner configuration is incomplete")
	}
	logger := r.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ticker := time.NewTicker(r.PollInterval)
	defer ticker.Stop()
	for {
		if err := r.ProcessBatch(ctx); err != nil {
			logger.ErrorContext(ctx, "outbox batch failed", slog.String("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *Runner) ProcessBatch(ctx context.Context) error {
	events, err := r.Store.Claim(ctx, r.WorkerID, r.BatchSize, r.Lease)
	if err != nil {
		return err
	}
	var failures []error
	for _, event := range events {
		if err := r.Handler.Handle(ctx, event); err != nil {
			if failErr := r.Store.Fail(ctx, event.ID, r.WorkerID, err, r.MaxAttempts, r.BaseBackoff); failErr != nil {
				failures = append(failures, failErr)
			}
			continue
		}
		if err := r.Store.Complete(ctx, event.ID, r.WorkerID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
