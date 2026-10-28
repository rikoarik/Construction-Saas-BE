-- +goose Up
INSERT INTO app.capabilities (code, description) VALUES
    ('platform.dashboard.read', 'Read platform-wide operational dashboard');
INSERT INTO app.platform_role_capabilities (role_code, capability_code) VALUES
    ('superadmin', 'platform.dashboard.read');
-- Legacy platform_admins remain an effective grant during the identity transition;
-- identity.Store.PlatformCapabilities grants this capability to those accounts too.

-- A failure time is not reconstructible from historical outbox rows.
ALTER TABLE app.outbox_events ADD COLUMN last_failed_at TIMESTAMPTZ;
CREATE INDEX outbox_failed_time_idx ON app.outbox_events (last_failed_at DESC, id DESC)
    WHERE last_failed_at IS NOT NULL;
CREATE INDEX companies_created_dashboard_idx ON app.companies (created_at DESC, id DESC);
CREATE INDEX users_created_dashboard_idx ON app.users (created_at);
CREATE INDEX audit_logs_platform_activity_idx ON app.audit_logs (occurred_at DESC, id DESC)
    WHERE action IN ('company.suspended', 'company.reactivated', 'subscription.changed');

-- +goose Down
DROP INDEX app.audit_logs_platform_activity_idx;
DROP INDEX app.users_created_dashboard_idx;
DROP INDEX app.companies_created_dashboard_idx;
DROP INDEX app.outbox_failed_time_idx;
ALTER TABLE app.outbox_events DROP COLUMN last_failed_at;
DELETE FROM app.platform_role_capabilities WHERE capability_code = 'platform.dashboard.read';
DELETE FROM app.capabilities WHERE code = 'platform.dashboard.read';
