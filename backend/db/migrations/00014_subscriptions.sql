-- +goose Up
-- Read-only subscription foundation for the platform dashboard (SA-005).
-- Plan definitions and company assignments only: entitlement overrides, usage
-- counters, billing metadata, and the SA-006 management endpoints are not part
-- of this migration, so no plan catalog is inserted here. Until an operator
-- assigns plans, the distribution widget reports an empty, available list.
CREATE TABLE app.plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT NOT NULL UNIQUE CHECK (code ~ '^[a-z][a-z0-9_-]{1,39}$'),
    name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'retired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE app.subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES app.companies(id) ON DELETE RESTRICT,
    plan_id UUID NOT NULL REFERENCES app.plans(id) ON DELETE RESTRICT,
    -- Lifecycle states the dashboard must distinguish. Grace period and read-only
    -- policies belong to SA-006 and are not modelled yet.
    status TEXT NOT NULL CHECK (status IN ('trialing', 'active', 'past_due', 'canceled')),
    period_start TIMESTAMPTZ NOT NULL DEFAULT now(),
    period_end TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_end IS NULL OR period_end > period_start)
);

-- A company has at most one current subscription; canceled rows remain as history.
CREATE UNIQUE INDEX subscriptions_one_current_per_company_idx ON app.subscriptions (company_id)
    WHERE status IN ('trialing', 'active', 'past_due');
CREATE INDEX subscriptions_current_plan_idx ON app.subscriptions (plan_id)
    WHERE status IN ('trialing', 'active', 'past_due');

-- +goose Down
DROP TABLE app.subscriptions;
DROP TABLE app.plans;
