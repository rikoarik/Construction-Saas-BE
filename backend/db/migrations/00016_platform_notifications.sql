-- +goose Up
-- Read markers for the platform notification centre (SA-005 header bell).
--
-- Notifications themselves are not stored: they are derived on read from records
-- the platform already keeps (failed/retry-pending outbox events, suspended
-- companies, past-due subscriptions). Storing a second copy of those facts would
-- let the two drift apart. Only the per-operator "already read" decision is new
-- state, so this table holds nothing but a marker.
--
-- A marker is scoped to the operator, never to a company: platform access is not
-- tenant membership, and one operator's read state must not affect another's.
CREATE TABLE app.platform_notification_reads (
    user_id UUID NOT NULL REFERENCES app.users(id) ON DELETE CASCADE,
    -- Stable key of the derived notification, e.g. "process:42" or
    -- "company:6f1c…". The format is enforced so a client cannot store arbitrary
    -- text here; unknown keys are harmless because the marker is only consulted
    -- when the same derived notification exists.
    notification_key TEXT NOT NULL CHECK (notification_key ~ '^(process|company|subscription):[A-Za-z0-9-]{1,64}$'),
    read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, notification_key)
);

-- The list query joins markers for one operator only, so user_id must lead.
CREATE INDEX platform_notification_reads_user_idx ON app.platform_notification_reads (user_id, read_at DESC);

-- +goose Down
DROP TABLE app.platform_notification_reads;
