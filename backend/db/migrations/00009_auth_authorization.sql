-- +goose Up
ALTER TABLE app.users
    ADD COLUMN email_verified_at TIMESTAMPTZ,
    ADD COLUMN security_version BIGINT NOT NULL DEFAULT 1 CHECK (security_version > 0);

CREATE INDEX users_status_idx ON app.users (status);

-- Credentials are authoritative in user_credentials. The legacy column remains
-- for a forward-only transition, and the trigger keeps both representations
-- consistent for callers that still update users directly during migration.
CREATE TABLE app.user_credentials (
    user_id UUID PRIMARY KEY REFERENCES app.users(id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL CHECK (length(password_hash) > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO app.user_credentials (user_id, password_hash, updated_at)
SELECT id, password_hash, updated_at FROM app.users;

-- +goose StatementBegin
CREATE FUNCTION app.sync_user_credentials()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO app.user_credentials (user_id, password_hash, updated_at)
    VALUES (NEW.id, NEW.password_hash, NEW.updated_at)
    ON CONFLICT (user_id) DO UPDATE
    SET password_hash = EXCLUDED.password_hash,
        updated_at = EXCLUDED.updated_at
    WHERE app.user_credentials.password_hash IS DISTINCT FROM EXCLUDED.password_hash;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER users_sync_credentials
AFTER INSERT OR UPDATE OF password_hash, updated_at ON app.users
FOR EACH ROW EXECUTE FUNCTION app.sync_user_credentials();

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM app.users u
        LEFT JOIN app.user_credentials c ON c.user_id = u.id
        WHERE c.user_id IS NULL OR c.password_hash IS DISTINCT FROM u.password_hash
    ) THEN
        RAISE EXCEPTION 'user credential backfill verification failed';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE app.auth_sessions
    ADD COLUMN auth_state TEXT NOT NULL DEFAULT 'active'
        CHECK (auth_state IN ('mfa_pending', 'active')),
    ADD COLUMN absolute_expires_at TIMESTAMPTZ,
    ADD COLUMN mfa_verified_at TIMESTAMPTZ,
    ADD COLUMN security_version BIGINT NOT NULL DEFAULT 1 CHECK (security_version > 0),
    ADD COLUMN mfa_failed_attempts INTEGER NOT NULL DEFAULT 0 CHECK (mfa_failed_attempts >= 0),
    ADD COLUMN last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN ip_hash TEXT,
    ADD COLUMN user_agent_hash TEXT;

UPDATE app.auth_sessions
SET absolute_expires_at = expires_at
WHERE absolute_expires_at IS NULL;

ALTER TABLE app.auth_sessions
    ALTER COLUMN absolute_expires_at SET NOT NULL,
    ADD CONSTRAINT auth_sessions_absolute_expiry_check
        CHECK (absolute_expires_at >= expires_at),
    ADD CONSTRAINT auth_sessions_mfa_state_check
        CHECK (auth_state = 'active' OR mfa_verified_at IS NULL);

-- +goose StatementBegin
CREATE FUNCTION app.fill_session_absolute_expiry()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.absolute_expires_at IS NULL THEN
        NEW.absolute_expires_at := NEW.expires_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER auth_sessions_fill_absolute_expiry
BEFORE INSERT OR UPDATE OF expires_at ON app.auth_sessions
FOR EACH ROW EXECUTE FUNCTION app.fill_session_absolute_expiry();

CREATE INDEX auth_sessions_pending_expiry_idx
    ON app.auth_sessions (expires_at)
    WHERE auth_state = 'mfa_pending' AND revoked_at IS NULL;

CREATE TABLE app.mfa_totp_factors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES app.users(id) ON DELETE CASCADE,
    secret_ciphertext BYTEA NOT NULL CHECK (octet_length(secret_ciphertext) > 16),
    secret_nonce BYTEA NOT NULL CHECK (octet_length(secret_nonce) = 12),
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'revoked')),
    enrollment_expires_at TIMESTAMPTZ,
    activated_at TIMESTAMPTZ,
    last_used_step BIGINT CHECK (last_used_step IS NULL OR last_used_step >= 0),
    failed_attempts INTEGER NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0),
    locked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (status <> 'pending' OR enrollment_expires_at IS NOT NULL),
    CHECK (status <> 'active' OR activated_at IS NOT NULL)
);

CREATE UNIQUE INDEX mfa_totp_one_active_per_user_idx
    ON app.mfa_totp_factors (user_id)
    WHERE status = 'active';
CREATE UNIQUE INDEX mfa_totp_one_pending_per_user_idx
    ON app.mfa_totp_factors (user_id)
    WHERE status = 'pending';
CREATE INDEX mfa_totp_pending_expiry_idx
    ON app.mfa_totp_factors (enrollment_expires_at)
    WHERE status = 'pending';

CREATE TABLE app.mfa_recovery_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES app.users(id) ON DELETE CASCADE,
    factor_id UUID NOT NULL REFERENCES app.mfa_totp_factors(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL UNIQUE CHECK (length(code_hash) = 64),
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX mfa_recovery_user_unused_idx
    ON app.mfa_recovery_codes (user_id)
    WHERE used_at IS NULL;

CREATE TABLE app.auth_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES app.users(id) ON DELETE CASCADE,
    purpose TEXT NOT NULL CHECK (purpose IN ('password_reset', 'email_verification')),
    token_hash TEXT NOT NULL UNIQUE CHECK (length(token_hash) = 64),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    requested_ip_hash TEXT,
    CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);

CREATE INDEX auth_tokens_user_purpose_active_idx
    ON app.auth_tokens (user_id, purpose, expires_at DESC)
    WHERE consumed_at IS NULL;

-- Raw delivery secrets are encrypted separately from the auth token hash.
-- The worker clears the ciphertext after a successful send; outbox payloads
-- contain only the delivery identifier.
CREATE TABLE app.auth_email_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES app.users(id) ON DELETE CASCADE,
    recipient_email TEXT NOT NULL CHECK (length(btrim(recipient_email)) > 0),
    template TEXT NOT NULL CHECK (template IN ('password_reset', 'email_verification', 'company_invitation')),
    token_ciphertext BYTEA,
    token_nonce BYTEA CHECK (token_nonce IS NULL OR octet_length(token_nonce) = 12),
    link_expires_at TIMESTAMPTZ NOT NULL,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((token_ciphertext IS NULL) = (token_nonce IS NULL)),
    CHECK (delivered_at IS NULL OR token_ciphertext IS NULL)
);

CREATE INDEX auth_email_deliveries_pending_idx
    ON app.auth_email_deliveries (created_at)
    WHERE delivered_at IS NULL;

CREATE TABLE app.security_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES app.users(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL CHECK (length(event_type) > 0),
    ip_hash TEXT,
    user_agent_hash TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX security_events_user_created_idx ON app.security_events (user_id, created_at DESC);
CREATE INDEX security_events_type_created_idx ON app.security_events (event_type, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION app.reject_security_event_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'security_events is append-only';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER security_events_append_only
BEFORE UPDATE OR DELETE ON app.security_events
FOR EACH ROW EXECUTE FUNCTION app.reject_security_event_mutation();

CREATE TABLE app.capabilities (
    code TEXT PRIMARY KEY CHECK (length(code) > 0),
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE app.platform_roles (
    code TEXT PRIMARY KEY CHECK (length(code) > 0),
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE app.platform_role_capabilities (
    role_code TEXT NOT NULL REFERENCES app.platform_roles(code) ON DELETE CASCADE,
    capability_code TEXT NOT NULL REFERENCES app.capabilities(code) ON DELETE CASCADE,
    PRIMARY KEY (role_code, capability_code)
);

CREATE TABLE app.platform_role_assignments (
    user_id UUID NOT NULL REFERENCES app.users(id) ON DELETE RESTRICT,
    role_code TEXT NOT NULL REFERENCES app.platform_roles(code) ON DELETE RESTRICT,
    granted_by UUID REFERENCES app.users(id) ON DELETE SET NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, role_code),
    CHECK (revoked_at IS NULL OR revoked_at >= granted_at)
);

CREATE INDEX platform_role_assignments_active_idx
    ON app.platform_role_assignments (user_id)
    WHERE revoked_at IS NULL;

INSERT INTO app.capabilities (code, description) VALUES
    ('platform.access', 'Enter the platform control plane'),
    ('platform.roles.manage', 'Grant and revoke platform roles'),
    ('company.read', 'Read company resources'),
    ('company.manage', 'Manage company settings'),
    ('member.read', 'Read company members'),
    ('member.manage', 'Invite, remove, and change company members'),
    ('project.read', 'Read project resources'),
    ('project.write', 'Create and change project resources');

INSERT INTO app.platform_roles (code, description) VALUES
    ('superadmin', 'Platform operator with full platform capabilities');

INSERT INTO app.platform_role_capabilities (role_code, capability_code) VALUES
    ('superadmin', 'platform.access'),
    ('superadmin', 'platform.roles.manage');

INSERT INTO app.platform_role_assignments (user_id, role_code, granted_at)
SELECT user_id, 'superadmin', granted_at FROM app.platform_admins
ON CONFLICT (user_id, role_code) DO NOTHING;

CREATE TABLE app.tenant_role_capabilities (
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member', 'viewer')),
    capability_code TEXT NOT NULL REFERENCES app.capabilities(code) ON DELETE CASCADE,
    PRIMARY KEY (role, capability_code)
);

INSERT INTO app.tenant_role_capabilities (role, capability_code) VALUES
    ('owner', 'company.read'), ('owner', 'company.manage'),
    ('owner', 'member.read'), ('owner', 'member.manage'),
    ('owner', 'project.read'), ('owner', 'project.write'),
    ('admin', 'company.read'), ('admin', 'member.read'), ('admin', 'member.manage'),
    ('admin', 'project.read'), ('admin', 'project.write'),
    ('member', 'company.read'), ('member', 'member.read'),
    ('member', 'project.read'), ('member', 'project.write'),
    ('viewer', 'company.read'), ('viewer', 'member.read'), ('viewer', 'project.read');

ALTER TABLE app.company_invitations
    ADD COLUMN revoked_at TIMESTAMPTZ,
    ADD COLUMN accepted_by UUID REFERENCES app.users(id) ON DELETE SET NULL,
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD CONSTRAINT company_invitations_revocation_check
        CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    ADD CONSTRAINT company_invitations_terminal_state_check
        CHECK (accepted_at IS NULL OR revoked_at IS NULL);

CREATE INDEX company_invitations_pending_idx
    ON app.company_invitations (company_id, email_normalized, expires_at DESC, id DESC)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE TABLE app.auth_rate_limits (
    scope TEXT NOT NULL CHECK (length(scope) > 0),
    key_hash TEXT NOT NULL CHECK (length(key_hash) = 64),
    window_start TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 1 CHECK (attempts > 0),
    PRIMARY KEY (scope, key_hash, window_start)
);

CREATE INDEX auth_rate_limits_window_idx ON app.auth_rate_limits (window_start);

-- +goose Down
DROP TABLE app.auth_rate_limits;
DROP INDEX app.company_invitations_pending_idx;
ALTER TABLE app.company_invitations
    DROP CONSTRAINT company_invitations_terminal_state_check,
    DROP CONSTRAINT company_invitations_revocation_check,
    DROP COLUMN updated_at,
    DROP COLUMN accepted_by,
    DROP COLUMN revoked_at;
DROP TABLE app.tenant_role_capabilities;
DROP TABLE app.platform_role_assignments;
DROP TABLE app.platform_role_capabilities;
DROP TABLE app.platform_roles;
DROP TABLE app.capabilities;
DROP TRIGGER app.security_events_append_only ON app.security_events;
DROP FUNCTION app.reject_security_event_mutation();
DROP TABLE app.security_events;
DROP TABLE app.auth_email_deliveries;
DROP TABLE app.auth_tokens;
DROP TABLE app.mfa_recovery_codes;
DROP INDEX app.mfa_totp_pending_expiry_idx;
DROP INDEX app.mfa_totp_one_pending_per_user_idx;
DROP INDEX app.mfa_totp_one_active_per_user_idx;
DROP TABLE app.mfa_totp_factors;
DROP INDEX app.auth_sessions_pending_expiry_idx;
DROP TRIGGER app.auth_sessions_fill_absolute_expiry ON app.auth_sessions;
DROP FUNCTION app.fill_session_absolute_expiry();
ALTER TABLE app.auth_sessions
    DROP CONSTRAINT auth_sessions_mfa_state_check,
    DROP CONSTRAINT auth_sessions_absolute_expiry_check,
    DROP COLUMN user_agent_hash,
    DROP COLUMN ip_hash,
    DROP COLUMN last_seen_at,
    DROP COLUMN mfa_failed_attempts,
    DROP COLUMN security_version,
    DROP COLUMN mfa_verified_at,
    DROP COLUMN absolute_expires_at,
    DROP COLUMN auth_state;
DROP TRIGGER app.users_sync_credentials ON app.users;
DROP FUNCTION app.sync_user_credentials();
DROP TABLE app.user_credentials;
DROP INDEX app.users_status_idx;
ALTER TABLE app.users
    DROP COLUMN security_version,
    DROP COLUMN email_verified_at;
