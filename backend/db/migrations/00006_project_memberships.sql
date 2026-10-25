-- +goose Up
CREATE TABLE app.projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES app.companies(id) ON DELETE RESTRICT,
    project_code TEXT NOT NULL CHECK (length(btrim(project_code)) BETWEEN 1 AND 80),
    name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 240),
    project_type TEXT NOT NULL CHECK (length(btrim(project_type)) BETWEEN 1 AND 80),
    timezone TEXT NOT NULL DEFAULT 'UTC',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(company_id, project_code),
    UNIQUE(company_id, id)
);

CREATE TABLE app.project_memberships (
    company_id UUID NOT NULL,
    project_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role TEXT NOT NULL CHECK (length(btrim(role)) BETWEEN 1 AND 80),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'removed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id),
    FOREIGN KEY (company_id, project_id) REFERENCES app.projects(company_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (company_id, user_id) REFERENCES app.company_memberships(company_id, user_id) ON DELETE RESTRICT
);

CREATE INDEX project_memberships_user_status_idx ON app.project_memberships (user_id, status, company_id, project_id);
CREATE INDEX project_memberships_project_role_idx ON app.project_memberships (company_id, project_id, status, role, user_id);

-- +goose Down
DROP TABLE app.project_memberships;
DROP TABLE app.projects;
