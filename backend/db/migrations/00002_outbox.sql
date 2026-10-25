-- +goose Up
CREATE TABLE app.outbox_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id UUID,
    event_type TEXT NOT NULL CHECK (event_type <> ''),
    payload JSONB NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE CHECK (idempotency_key <> ''),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'delivered', 'dead')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_owner TEXT,
    lease_until TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at TIMESTAMPTZ,
    CHECK (
        (status = 'processing' AND lease_owner IS NOT NULL AND lease_until IS NOT NULL)
        OR (status <> 'processing' AND lease_owner IS NULL AND lease_until IS NULL)
    ),
    CHECK ((status = 'delivered') = (delivered_at IS NOT NULL))
);

CREATE INDEX outbox_pending_available_idx ON app.outbox_events (available_at, id) WHERE status = 'pending';
CREATE INDEX outbox_expired_lease_idx ON app.outbox_events (lease_until, id) WHERE status = 'processing';
CREATE INDEX outbox_dead_created_idx ON app.outbox_events (created_at, id) WHERE status = 'dead';

-- +goose Down
DROP TABLE app.outbox_events;
