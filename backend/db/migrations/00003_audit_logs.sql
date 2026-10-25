-- +goose Up
CREATE TABLE app.audit_logs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    company_id UUID NOT NULL,
    project_id UUID,
    actor_id UUID,
    action TEXT NOT NULL CHECK (length(action) BETWEEN 1 AND 128),
    entity_type TEXT NOT NULL CHECK (length(entity_type) BETWEEN 1 AND 128),
    entity_id UUID,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    request_id TEXT CHECK (request_id IS NULL OR length(request_id) <= 128),
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata_json) = 'object'),
    schema_version SMALLINT NOT NULL DEFAULT 1 CHECK (schema_version > 0)
);

CREATE INDEX audit_logs_company_time_idx ON app.audit_logs (company_id, occurred_at DESC, id DESC);
CREATE INDEX audit_logs_project_time_idx ON app.audit_logs (company_id, project_id, occurred_at DESC, id DESC) WHERE project_id IS NOT NULL;
CREATE INDEX audit_logs_entity_time_idx ON app.audit_logs (company_id, entity_type, entity_id, occurred_at DESC, id DESC) WHERE entity_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION app.reject_audit_log_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    RAISE EXCEPTION 'audit logs are append-only' USING ERRCODE = '55000';
END;
$function$;
-- +goose StatementEnd

CREATE TRIGGER audit_logs_append_only
BEFORE UPDATE OR DELETE ON app.audit_logs
FOR EACH ROW EXECUTE FUNCTION app.reject_audit_log_mutation();

-- +goose Down
DROP TRIGGER audit_logs_append_only ON app.audit_logs;
DROP FUNCTION app.reject_audit_log_mutation();
DROP TABLE app.audit_logs;
