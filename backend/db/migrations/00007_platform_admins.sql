-- +goose Up
CREATE TABLE app.platform_admins (
    user_id UUID PRIMARY KEY REFERENCES app.users(id) ON DELETE RESTRICT,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE app.platform_admins;
