package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Heartbeat records an operational observation, not a guarantee that a worker
// can successfully deliver an event to its external provider.
func Heartbeat(ctx context.Context, pool *pgxpool.Pool, workerID, eventType string) error {
	if pool == nil || workerID == "" || len(workerID) > 200 || eventType == "" {
		return errors.New("worker heartbeat requires a pool, worker ID, and event type")
	}
	heartbeatCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := pool.Exec(heartbeatCtx, `INSERT INTO app.worker_heartbeats (worker_id, event_type, checked_at)
 VALUES ($1, $2, now()) ON CONFLICT (worker_id) DO UPDATE
 SET event_type=EXCLUDED.event_type, checked_at=EXCLUDED.checked_at`, workerID, eventType)
	if err != nil {
		return fmt.Errorf("record worker heartbeat: %w", err)
	}
	return nil
}

func RemoveHeartbeat(ctx context.Context, pool *pgxpool.Pool, workerID string) error {
	if pool == nil || workerID == "" {
		return errors.New("worker heartbeat requires a pool and worker ID")
	}
	_, err := pool.Exec(ctx, `DELETE FROM app.worker_heartbeats WHERE worker_id=$1`, workerID)
	if err != nil {
		return fmt.Errorf("remove worker heartbeat: %w", err)
	}
	return nil
}
