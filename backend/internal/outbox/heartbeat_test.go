package outbox

import (
	"context"
	"testing"
	"time"
)

func TestHeartbeatPersistsAndRemoves(t *testing.T) {
	_, pool := testStore(t)
	ctx := context.Background()
	id := "heartbeat-test-" + time.Now().Format("20060102150405.000000000")
	if err := Heartbeat(ctx, pool, id, "email.delivery"); err != nil {
		t.Fatal(err)
	}
	defer RemoveHeartbeat(ctx, pool, id)
	var first time.Time
	if err := pool.QueryRow(ctx, `SELECT checked_at FROM app.worker_heartbeats WHERE worker_id=$1`, id).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE app.worker_heartbeats SET checked_at=now()-interval '2 minutes' WHERE worker_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err := Heartbeat(ctx, pool, id, "email.delivery"); err != nil {
		t.Fatal(err)
	}
	var updated time.Time
	if err := pool.QueryRow(ctx, `SELECT checked_at FROM app.worker_heartbeats WHERE worker_id=$1`, id).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.Before(first) {
		t.Fatalf("heartbeat went backwards: %s -> %s", first, updated)
	}
	if err := RemoveHeartbeat(ctx, pool, id); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM app.worker_heartbeats WHERE worker_id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("heartbeat removal: %d %v", count, err)
	}
}
