-- +goose Up
CREATE TABLE app.users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL CHECK (length(btrim(email)) > 0),
    email_normalized TEXT NOT NULL CHECK (length(btrim(email_normalized)) > 0),
    password_hash TEXT NOT NULL CHECK (length(password_hash) > 0),
    status TEXT NOT NULL CHECK (length(status) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (email_normalized = lower(btrim(email)))
);

CREATE UNIQUE INDEX users_email_normalized_idx ON app.users (email_normalized);

CREATE TABLE app.auth_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES app.users(id) ON DELETE RESTRICT,
    token_hash TEXT NOT NULL UNIQUE CHECK (length(token_hash) > 0),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE INDEX auth_sessions_user_expiry_idx ON app.auth_sessions (user_id, expires_at DESC);
CREATE INDEX auth_sessions_active_expiry_idx ON app.auth_sessions (expires_at) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE app.auth_sessions;
DROP TABLE app.users;
