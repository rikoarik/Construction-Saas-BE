-- +goose Up
-- Operational liveness only. No outbox payload or tenant content is stored here.
CREATE TABLE app.worker_heartbeats (
    worker_id TEXT PRIMARY KEY CHECK (length(worker_id) BETWEEN 1 AND 200),
    event_type TEXT NOT NULL CHECK (event_type <> ''),
    checked_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX worker_heartbeats_type_checked_idx ON app.worker_heartbeats (event_type, checked_at DESC);

-- +goose Down
DROP TABLE app.worker_heartbeats;
