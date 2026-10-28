package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type recordingExec struct {
	calls []recordedCount
	err   error
}

type recordedCount struct {
	day   string
	count int64
}

func (r *recordingExec) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	if r.err != nil {
		return pgconn.CommandTag{}, r.err
	}
	r.calls = append(r.calls, recordedCount{day: args[0].(string), count: args[1].(int64)})
	return pgconn.CommandTag{}, nil
}

func TestAPIRequestMeterAggregatesPerUTCDay(t *testing.T) {
	exec := &recordingExec{}
	meter := NewAPIRequestMeter(exec)
	// Local midnight in a +7 zone is still the previous UTC day.
	clock := time.Date(2026, 10, 29, 6, 30, 0, 0, time.FixedZone("WIB", 7*3600))
	meter.Clock = func() time.Time { return clock }

	for range 3 {
		meter.Record()
	}
	clock = clock.Add(24 * time.Hour)
	meter.Record()

	// Nothing is written until a flush, so a request never waits on the database.
	if len(exec.calls) != 0 {
		t.Fatalf("wrote during Record: %+v", exec.calls)
	}
	if err := meter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	counts := map[string]int64{}
	for _, call := range exec.calls {
		counts[call.day] = call.count
	}
	if counts["2026-10-28"] != 3 || counts["2026-10-29"] != 1 {
		t.Fatalf("recorded counts = %v", counts)
	}

	// A second flush has nothing left to write.
	before := len(exec.calls)
	if err := meter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(exec.calls) != before {
		t.Fatalf("flushed an empty buffer: %+v", exec.calls)
	}
}

func TestAPIRequestMeterRetainsCountsWhenFlushFails(t *testing.T) {
	exec := &recordingExec{err: errors.New("database unavailable")}
	meter := NewAPIRequestMeter(exec)
	meter.Clock = func() time.Time { return time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC) }
	meter.Record()
	meter.Record()

	if err := meter.Flush(context.Background()); err == nil {
		t.Fatal("expected the failed write to surface")
	}

	// The counts survive, so a transient error delays the numbers instead of
	// discarding them.
	exec.err = nil
	if err := meter.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(exec.calls) != 1 || exec.calls[0].count != 2 || exec.calls[0].day != "2026-10-29" {
		t.Fatalf("recovered counts = %+v", exec.calls)
	}
}
