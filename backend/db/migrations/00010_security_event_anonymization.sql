-- +goose Up
-- The original append-only trigger also blocked the foreign key's
-- ON DELETE SET NULL action, making user deletion impossible. Keep events
-- immutable while allowing only the system anonymization update.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app.reject_security_event_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.user_id IS NOT NULL
       AND NEW.user_id IS NULL
       AND NEW.id = OLD.id
       AND NEW.event_type = OLD.event_type
       AND NEW.ip_hash IS NOT DISTINCT FROM OLD.ip_hash
       AND NEW.user_agent_hash IS NOT DISTINCT FROM OLD.user_agent_hash
       AND NEW.metadata = OLD.metadata
       AND NEW.created_at = OLD.created_at
    THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'security_events is append-only';
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION app.reject_security_event_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'security_events is append-only';
END;
$$;
-- +goose StatementEnd
