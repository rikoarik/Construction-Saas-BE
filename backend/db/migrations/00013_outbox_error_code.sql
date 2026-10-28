-- +goose Up
-- Safe operational classification. Raw provider errors and event payloads stay private.
ALTER TABLE app.outbox_events ADD COLUMN error_code TEXT
    CHECK (error_code IS NULL OR error_code IN ('JOB_EXECUTION_FAILED'));

-- +goose Down
ALTER TABLE app.outbox_events DROP COLUMN error_code;
