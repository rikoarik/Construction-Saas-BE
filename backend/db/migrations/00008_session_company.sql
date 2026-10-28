-- +goose Up
ALTER TABLE app.auth_sessions ADD COLUMN selected_company_id UUID REFERENCES app.companies(id) ON DELETE RESTRICT;

-- +goose Down
ALTER TABLE app.auth_sessions DROP COLUMN selected_company_id;
