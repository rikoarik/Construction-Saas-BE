-- +goose Up
-- Daily API request counters for the platform usage card. Requests are
-- aggregated per UTC day instead of stored individually: the dashboard only
-- needs a total, and a per-request row would grow without bound while carrying
-- no information the platform is allowed to surface.
CREATE TABLE app.api_request_daily (
    day DATE PRIMARY KEY,
    requests BIGINT NOT NULL DEFAULT 0 CHECK (requests >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE app.api_request_daily;
