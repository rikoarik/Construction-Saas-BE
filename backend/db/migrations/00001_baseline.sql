-- +goose Up
CREATE SCHEMA IF NOT EXISTS app;

-- +goose Down
DROP SCHEMA IF EXISTS app;
