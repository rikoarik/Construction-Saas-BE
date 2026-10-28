-- +goose Up
ALTER TABLE app.companies
    ADD COLUMN code TEXT,
    ADD COLUMN legal_name TEXT,
    ADD COLUMN contact_email TEXT,
    ADD COLUMN phone TEXT,
    ADD COLUMN address TEXT,
    ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC',
    ADD COLUMN plan_id UUID REFERENCES app.plans(id) ON DELETE RESTRICT,
    ADD COLUMN access_version BIGINT NOT NULL DEFAULT 1 CHECK (access_version > 0),
    ADD COLUMN suspended_reason TEXT,
    ADD COLUMN suspended_by UUID REFERENCES app.users(id) ON DELETE RESTRICT,
    ADD COLUMN suspended_at TIMESTAMPTZ;
UPDATE app.companies SET code = CASE WHEN slug ~ '^[a-z][a-z0-9_-]{1,39}$' THEN slug ELSE 'c_' || replace(id::text,'-','') END;
ALTER TABLE app.companies ALTER COLUMN code SET NOT NULL;
ALTER TABLE app.companies ADD CONSTRAINT companies_code_format CHECK (code ~ '^[a-z][a-z0-9_-]{1,39}$');
ALTER TABLE app.companies ADD CONSTRAINT companies_status_v2 CHECK (status IN ('provisioning','active','suspended','archived'));
ALTER TABLE app.companies DROP CONSTRAINT companies_status_check;
-- The owner uniqueness invariant stays in place; platform admin updates
-- cannot deactivate the last owner even when another admin remains active.
CREATE UNIQUE INDEX companies_code_unique ON app.companies (code);
CREATE INDEX companies_status_created_idx ON app.companies (status, created_at DESC, id DESC);
CREATE INDEX companies_plan_created_idx ON app.companies (plan_id, created_at DESC, id DESC);
ALTER TABLE app.company_invitations ADD COLUMN delivery_id UUID REFERENCES app.auth_email_deliveries(id) ON DELETE SET NULL;
UPDATE app.company_invitations SET revoked_at=now(),updated_at=now()
WHERE accepted_at IS NULL AND revoked_at IS NULL AND expires_at<=now();
WITH ranked AS (
    SELECT id, row_number() OVER (PARTITION BY company_id,email_normalized ORDER BY created_at DESC,id DESC) AS n
    FROM app.company_invitations WHERE accepted_at IS NULL AND revoked_at IS NULL
)
UPDATE app.company_invitations i SET revoked_at=now(),updated_at=now()
FROM ranked r WHERE r.id=i.id AND r.n>1;
CREATE UNIQUE INDEX company_invitations_pending_unique ON app.company_invitations (company_id,email_normalized)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;
CREATE TABLE app.platform_idempotency (
    actor_id UUID NOT NULL REFERENCES app.users(id) ON DELETE RESTRICT,
    operation TEXT NOT NULL,
    key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_json JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (actor_id,operation,key),
    CHECK (length(key) BETWEEN 1 AND 128)
);
INSERT INTO app.capabilities (code,description) VALUES
    ('platform.company.read','Read company metadata'),
    ('platform.company.create','Create companies'),
    ('platform.company.update','Update company metadata'),
    ('platform.company.suspend','Suspend companies'),
    ('platform.company.reactivate','Reactivate companies'),
    ('platform.member.read','Read company administrators'),
    ('platform.member.invite','Invite company administrators'),
    ('platform.member.revoke','Revoke invitations and update administrators');
INSERT INTO app.platform_role_capabilities (role_code,capability_code)
SELECT 'superadmin',code FROM app.capabilities WHERE code LIKE 'platform.company.%' OR code LIKE 'platform.member.%';

-- +goose Down
DELETE FROM app.platform_role_capabilities WHERE capability_code LIKE 'platform.company.%' OR capability_code LIKE 'platform.member.%';
DELETE FROM app.capabilities WHERE code LIKE 'platform.company.%' OR code LIKE 'platform.member.%';
DROP TABLE app.platform_idempotency;
-- Preserve the existing one-active-owner index on rollback.
DROP INDEX app.company_invitations_pending_unique;
ALTER TABLE app.company_invitations DROP COLUMN delivery_id;
DROP INDEX app.companies_plan_created_idx;
DROP INDEX app.companies_status_created_idx;
DROP INDEX app.companies_code_unique;
ALTER TABLE app.companies ADD CONSTRAINT companies_status_check CHECK (status IN ('active','suspended'));
ALTER TABLE app.companies DROP CONSTRAINT companies_status_v2;
ALTER TABLE app.companies DROP CONSTRAINT companies_code_format;
ALTER TABLE app.companies DROP COLUMN code, DROP COLUMN legal_name, DROP COLUMN contact_email,
    DROP COLUMN phone, DROP COLUMN address, DROP COLUMN timezone, DROP COLUMN plan_id,
    DROP COLUMN access_version, DROP COLUMN suspended_reason, DROP COLUMN suspended_by,
    DROP COLUMN suspended_at;
