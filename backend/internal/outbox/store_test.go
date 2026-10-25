package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"construction-saas/backend/internal/database"
)

type handlerFunc func(context.Context, Event) error

func (f handlerFunc) Handle(ctx context.Context, event Event) error { return f(ctx, event) }

func testStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := database.Open(ctx, url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(pool.Close)
	store, err := NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	return store, pool
}

func enqueueForTest(t *testing.T, store *Store, pool *pgxpool.Pool, key string) int64 {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	id, err := store.Enqueue(ctx, tx, nil, "test.event", json.RawMessage(`{"ok":true}`), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestEnqueueRequiresTransactionAndValidPayload(t *testing.T) {
	store := &Store{}
	for _, tc := range []struct {
		tx      pgx.Tx
		typ     string
		payload json.RawMessage
		key     string
	}{
		{typ: "event", payload: json.RawMessage(`{}`), key: "key"},
		{tx: &fakeTx{}, typ: "event", payload: json.RawMessage(`{invalid}`), key: "key"},
		{tx: &fakeTx{}, typ: "", payload: json.RawMessage(`{}`), key: "key"},
		{tx: &fakeTx{}, typ: "event", payload: json.RawMessage(`{}`), key: ""},
	} {
		if _, err := store.Enqueue(context.Background(), tc.tx, nil, tc.typ, tc.payload, tc.key); !errors.Is(err, ErrInvalid) {
			t.Errorf("Enqueue error = %v, want ErrInvalid", err)
		}
	}
}

type fakeTx struct{ pgx.Tx }

func TestClaimValidation(t *testing.T) {
	store := &Store{}
	if _, err := store.Claim(context.Background(), "", 1, time.Second); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}

func TestOutboxRetryDeadLetterAndIdempotency(t *testing.T) {
	store, pool := testStore(t)
	ctx := context.Background()
	key := "outbox-test-" + time.Now().Format("20060102150405.000000000")
	id := enqueueForTest(t, store, pool, key)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	duplicateID, err := store.Enqueue(ctx, tx, nil, "test.event", json.RawMessage(`{"ok":true}`), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if duplicateID != id {
		t.Fatalf("duplicate ID = %d, want %d", duplicateID, id)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enqueue(ctx, tx, nil, "test.event", json.RawMessage(`{"different":true}`), key); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("mismatched idempotency key = %v, want ErrIdempotencyConflict", err)
	}
	_ = tx.Rollback(ctx)

	claimed, err := store.Claim(ctx, "worker-a", 1, time.Second)
	if err != nil || len(claimed) != 1 || claimed[0].ID != id {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	if err := store.Fail(ctx, id, "wrong-worker", errors.New("failure"), 2, time.Millisecond); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("wrong worker failure = %v, want ErrLeaseLost", err)
	}
	if err := store.Fail(ctx, id, "worker-a", errors.New("first failure"), 2, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM app.outbox_events WHERE id=$1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("status after retry = %q, want pending", status)
	}
	if _, err := pool.Exec(ctx, `UPDATE app.outbox_events SET available_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	claimed, err = store.Claim(ctx, "worker-a", 1, time.Second)
	if err != nil || len(claimed) != 1 || claimed[0].Attempts != 2 {
		t.Fatalf("second claim = %#v, %v", claimed, err)
	}
	if err := store.Fail(ctx, id, "worker-a", errors.New("exhausted"), 2, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	dead, err := store.DeadLetters(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range dead {
		if event.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("dead letter %d not listed", id)
	}
}

func TestClaimExcludesConcurrentWorkerAndCompleteNeedsOwner(t *testing.T) {
	store, pool := testStore(t)
	id := enqueueForTest(t, store, pool, "claim-test-"+time.Now().Format("20060102150405.000000000"))
	ctx := context.Background()
	first, err := store.Claim(ctx, "worker-a", 1, time.Second)
	if err != nil || len(first) != 1 || first[0].ID != id {
		t.Fatalf("first claim = %#v, %v", first, err)
	}
	second, err := store.Claim(ctx, "worker-b", 1, time.Second)
	if err != nil || len(second) != 0 {
		t.Fatalf("second claim = %#v, %v; want empty", second, err)
	}
	if err := store.Complete(ctx, id, "worker-b"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("wrong owner complete = %v, want ErrLeaseLost", err)
	}
	if err := store.Complete(ctx, id, "worker-a"); err != nil {
		t.Fatal(err)
	}
}

func TestRunnerProcessesSuccessAndSchedulesFailure(t *testing.T) {
	store, pool := testStore(t)
	ctx := context.Background()
	successID := enqueueForTest(t, store, pool, "runner-ok-"+time.Now().Format("20060102150405.000000000"))
	failureID := enqueueForTest(t, store, pool, "runner-fail-"+time.Now().Format("20060102150405.000000000"))
	runner := Runner{
		Store: store, WorkerID: "runner", BatchSize: 10, Lease: time.Second,
		MaxAttempts: 3, BaseBackoff: time.Millisecond,
		Handler: handlerFunc(func(_ context.Context, event Event) error {
			if event.ID == failureID {
				return errors.New("transient")
			}
			return nil
		}),
	}
	if err := runner.ProcessBatch(ctx); err != nil {
		t.Fatal(err)
	}
	var successStatus, failureStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM app.outbox_events WHERE id=$1`, successID).Scan(&successStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM app.outbox_events WHERE id=$1`, failureID).Scan(&failureStatus); err != nil {
		t.Fatal(err)
	}
	if successStatus != "delivered" || failureStatus != "pending" {
		t.Fatalf("statuses = (%q, %q), want (delivered, pending)", successStatus, failureStatus)
	}
}

func TestEnqueueParticipatesInCallerTransaction(t *testing.T) {
	store, pool := testStore(t)
	ctx := context.Background()
	key := "rollback-test-" + time.Now().Format("20060102150405.000000000")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enqueue(ctx, tx, nil, "test.event", json.RawMessage(`{"ok":true}`), key); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM app.outbox_events WHERE idempotency_key=$1`, key).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back enqueue count = %d, want 0", count)
	}
}

func TestExpiredLeaseCanBeReclaimedAndOldOwnerRejected(t *testing.T) {
	store, pool := testStore(t)
	id := enqueueForTest(t, store, pool, "lease-test-"+time.Now().Format("20060102150405.000000000"))
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE app.outbox_events SET available_at=now()-interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.Claim(ctx, "worker-old", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range claimed {
		if event.ID == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("first claim = %#v, missing event %d", claimed, id)
	}
	if _, err := pool.Exec(ctx, `UPDATE app.outbox_events SET lease_until=now()-interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	second, err := store.Claim(ctx, "worker-new", 1, time.Minute)
	if err != nil || len(second) != 1 || second[0].ID != id {
		t.Fatalf("reclaim = %#v, %v", second, err)
	}
	if err := store.Complete(ctx, id, "worker-old"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("old owner complete = %v, want ErrLeaseLost", err)
	}
	if err := store.Complete(ctx, id, "worker-new"); err != nil {
		t.Fatal(err)
	}
}
