-- +goose Up
CREATE TABLE app.companies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    slug TEXT NOT NULL CHECK (length(slug) BETWEEN 1 AND 120 AND slug = lower(slug)),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (slug)
);

CREATE TABLE app.company_memberships (
    company_id UUID NOT NULL REFERENCES app.companies(id) ON DELETE RESTRICT,
    user_id UUID NOT NULL REFERENCES app.users(id) ON DELETE RESTRICT,
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member', 'viewer')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'removed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, user_id)
);

CREATE INDEX company_memberships_user_status_idx ON app.company_memberships (user_id, status, company_id);
CREATE INDEX company_memberships_company_status_idx ON app.company_memberships (company_id, status, role, user_id);
CREATE UNIQUE INDEX company_memberships_one_active_owner_idx ON app.company_memberships (company_id) WHERE role = 'owner' AND status = 'active';

CREATE TABLE app.company_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES app.companies(id) ON DELETE RESTRICT,
    email TEXT NOT NULL CHECK (length(btrim(email)) > 0),
    email_normalized TEXT NOT NULL CHECK (length(btrim(email_normalized)) > 0),
    token_hash TEXT NOT NULL UNIQUE CHECK (length(token_hash) > 0),
    role TEXT NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ,
    created_by UUID NOT NULL REFERENCES app.users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (email_normalized = lower(btrim(email))),
    CHECK (expires_at > created_at),
    CHECK (accepted_at IS NULL OR accepted_at >= created_at)
);

CREATE INDEX company_invitations_company_expiry_idx ON app.company_invitations (company_id, expires_at DESC, id DESC);
CREATE INDEX company_invitations_email_idx ON app.company_invitations (company_id, email_normalized, expires_at DESC);

CREATE TABLE app.company_settings (
    company_id UUID PRIMARY KEY REFERENCES app.companies(id) ON DELETE RESTRICT,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    locale TEXT NOT NULL DEFAULT 'en',
    defaults_json JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(defaults_json) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE app.company_settings;
DROP TABLE app.company_invitations;
DROP TABLE app.company_memberships;
DROP TABLE app.companies;
