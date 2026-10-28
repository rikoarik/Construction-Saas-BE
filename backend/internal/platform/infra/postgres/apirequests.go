package postgres

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// APIRequestMeter counts API requests per UTC day. Counting is buffered in
// memory and written in batches, so a request never waits on a write and a
// burst of traffic collapses into one upsert per day.
//
// Buffered counts live only in this process. A crash between flushes loses at
// most the last flush interval; the dashboard therefore reports recorded
// requests, not a guaranteed exact total.
type APIRequestMeter struct {
	Pool  execer
	Clock func() time.Time

	mu      sync.Mutex
	pending map[string]int64
}

// NewAPIRequestMeter returns a meter that has not recorded anything yet.
func NewAPIRequestMeter(pool execer) *APIRequestMeter {
	return &APIRequestMeter{Pool: pool, pending: make(map[string]int64)}
}

func (m *APIRequestMeter) now() time.Time {
	if m.Clock != nil {
		return m.Clock().UTC()
	}
	return time.Now().UTC()
}

// Record counts one request against the current UTC day. It takes a lock and
// returns; it never touches the database.
func (m *APIRequestMeter) Record() {
	day := m.now().Format(time.DateOnly)
	m.mu.Lock()
	m.pending[day]++
	m.mu.Unlock()
}

// Flush writes every buffered count. Counts leave the buffer before the write
// and are restored if it fails, so a transient database error delays the
// numbers instead of discarding them.
func (m *APIRequestMeter) Flush(ctx context.Context) error {
	m.mu.Lock()
	batch := m.pending
	m.pending = make(map[string]int64)
	m.mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	if err := m.write(ctx, batch); err != nil {
		m.mu.Lock()
		for day, count := range batch {
			m.pending[day] += count
		}
		m.mu.Unlock()
		return err
	}
	return nil
}

func (m *APIRequestMeter) write(ctx context.Context, batch map[string]int64) error {
	for day, count := range batch {
		// Adding to an existing row keeps the counter monotonic across restarts.
		if _, err := m.Pool.Exec(ctx, `INSERT INTO app.api_request_daily (day, requests)
VALUES ($1::date, $2)
ON CONFLICT (day) DO UPDATE
SET requests = app.api_request_daily.requests + EXCLUDED.requests, updated_at = now()`, day, count); err != nil {
			return fmt.Errorf("record api requests for %s: %w", day, err)
		}
	}
	return nil
}

// Run flushes on every tick and once more when the context is cancelled, so a
// graceful shutdown does not drop the counts buffered since the last tick.
func (m *APIRequestMeter) Run(ctx context.Context, every time.Duration, onError func(error)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := m.Flush(flushCtx); err != nil && onError != nil {
				onError(err)
			}
			return
		case <-ticker.C:
			flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := m.Flush(flushCtx)
			cancel()
			if err != nil && onError != nil {
				onError(err)
			}
		}
	}
}
