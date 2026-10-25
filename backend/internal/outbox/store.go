package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrLeaseLost           = errors.New("outbox lease lost")
	ErrInvalid             = errors.New("invalid outbox operation")
	ErrIdempotencyConflict = errors.New("outbox idempotency key reused with different event")
)

// Event is an outbox record claimed by a worker.
type Event struct {
	ID             int64           `json:"id"`
	CompanyID      *string         `json:"company_id,omitempty"`
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey string          `json:"idempotency_key"`
	Attempts       int             `json:"attempts"`
	LeaseOwner     string          `json:"-"`
}

// Store persists transactional outbox events in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, fmt.Errorf("%w: PostgreSQL pool is required", ErrInvalid)
	}
	return &Store{pool: pool}, nil
}

// Enqueue inserts an event; a repeated idempotency key returns the original ID.
func (s *Store) Enqueue(ctx context.Context, tx pgx.Tx, companyID *string, eventType string, payload json.RawMessage, key string) (int64, error) {
	if tx == nil || eventType == "" || key == "" || !json.Valid(payload) {
		return 0, fmt.Errorf("%w: transaction, event type, key, and valid JSON payload are required", ErrInvalid)
	}
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO app.outbox_events (company_id, event_type, payload, idempotency_key)
		VALUES ($1::uuid, $2, $3::jsonb, $4)
		ON CONFLICT (idempotency_key) DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
		WHERE app.outbox_events.company_id IS NOT DISTINCT FROM EXCLUDED.company_id
		  AND app.outbox_events.event_type = EXCLUDED.event_type
		  AND app.outbox_events.payload = EXCLUDED.payload
		RETURNING id`, companyID, eventType, payload, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrIdempotencyConflict
	}
	if err != nil {
		return 0, fmt.Errorf("enqueue outbox event: %w", err)
	}
	return id, nil
}

// Claim claims a batch using SKIP LOCKED. Expired leases are eligible for reclaim.
func (s *Store) Claim(ctx context.Context, worker string, limit int, lease time.Duration) ([]Event, error) {
	if worker == "" || limit < 1 || lease <= 0 {
		return nil, fmt.Errorf("%w: worker, positive limit, and positive lease are required", ErrInvalid)
	}
	rows, err := s.pool.Query(ctx, `
		WITH candidates AS (
			SELECT id
			FROM app.outbox_events
			WHERE (status = 'pending' AND available_at <= now())
			   OR (status = 'processing' AND lease_until <= now())
			ORDER BY available_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE app.outbox_events AS event
		SET status = 'processing', lease_owner = $2, lease_until = now() + $3::interval,
		    attempts = attempts + 1
		FROM candidates
		WHERE event.id = candidates.id
		RETURNING event.id, event.company_id::text, event.event_type, event.payload,
		          event.idempotency_key, event.attempts, event.lease_owner`, limit, worker, lease.String())
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	defer rows.Close()

	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		var companyID *string
		if err := rows.Scan(&event.ID, &companyID, &event.Type, &event.Payload, &event.IdempotencyKey, &event.Attempts, &event.LeaseOwner); err != nil {
			return nil, fmt.Errorf("scan claimed event: %w", err)
		}
		event.CompanyID = companyID
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed events: %w", err)
	}
	return events, nil
}

func (s *Store) Complete(ctx context.Context, id int64, worker string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE app.outbox_events
		SET status = 'delivered', delivered_at = now(), lease_owner = NULL, lease_until = NULL, last_error = NULL
		WHERE id = $1 AND status = 'processing' AND lease_owner = $2 AND lease_until > now()`, id, worker)
	if err != nil {
		return fmt.Errorf("complete outbox event: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

// Fail schedules retry with exponential backoff or moves exhausted work to dead letter.
func (s *Store) Fail(ctx context.Context, id int64, worker string, cause error, maxAttempts int, baseBackoff time.Duration) error {
	if cause == nil || maxAttempts < 1 || baseBackoff <= 0 {
		return fmt.Errorf("%w: cause, positive max attempts, and positive backoff are required", ErrInvalid)
	}
	var attempts int
	err := s.pool.QueryRow(ctx, `SELECT attempts FROM app.outbox_events WHERE id = $1 AND status = 'processing' AND lease_owner = $2 AND lease_until > now()`, id, worker).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLeaseLost
	}
	if err != nil {
		return fmt.Errorf("read outbox attempts: %w", err)
	}
	var tag pgconn.CommandTag
	if attempts >= maxAttempts {
		tag, err = s.pool.Exec(ctx, `
			UPDATE app.outbox_events SET status = 'dead', lease_owner = NULL, lease_until = NULL, last_error = $3
			WHERE id = $1 AND status = 'processing' AND lease_owner = $2 AND lease_until > now()`, id, worker, cause.Error())
	} else {
		backoff := baseBackoff
		for i := 1; i < attempts && backoff < time.Minute; i++ {
			if backoff > time.Duration(math.MaxInt64/2) {
				backoff = time.Minute
				break
			}
			backoff *= 2
		}
		if backoff > time.Minute {
			backoff = time.Minute
		}
		tag, err = s.pool.Exec(ctx, `
				UPDATE app.outbox_events
				SET status = 'pending', available_at = now() + $3::interval, lease_owner = NULL, lease_until = NULL, last_error = $4
				WHERE id = $1 AND status = 'processing' AND lease_owner = $2 AND lease_until > now()`, id, worker, backoff.String(), cause.Error())
	}
	if err != nil {
		return fmt.Errorf("fail outbox event: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

// DeadLetters returns a bounded inspection page of exhausted events.
func (s *Store) DeadLetters(ctx context.Context, limit int) ([]Event, error) {
	if limit < 1 {
		return nil, fmt.Errorf("%w: positive limit is required", ErrInvalid)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, company_id::text, event_type, payload, idempotency_key, attempts
		FROM app.outbox_events WHERE status = 'dead' ORDER BY created_at, id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list dead-letter events: %w", err)
	}
	defer rows.Close()
	results := make([]Event, 0)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.CompanyID, &event.Type, &event.Payload, &event.IdempotencyKey, &event.Attempts); err != nil {
			return nil, fmt.Errorf("scan dead-letter event: %w", err)
		}
		results = append(results, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dead-letter events: %w", err)
	}
	return results, nil
}
