-- Local demo dataset for the platform dashboard.
--
-- The script is insert-only and idempotent: every row carries a fixed UUID or a
-- fixed unique key, so a second run leaves existing rows untouched. It reads the
-- tables the dashboard queries (companies, users, memberships, projects, plans,
-- subscriptions, audit_logs, outbox_events) and nothing else.
--
-- Demo accounts use the reserved @seed.invalid domain and an Argon2id-shaped hash
-- with no known preimage, so they can never sign in. Never run this against a
-- database that holds real tenant data.

-- 54 staff accounts spread over the last 13 months. Company N draws its members
-- from a fixed window of this range, so overlapping staff belong to two companies.
INSERT INTO app.users (id, email, email_normalized, password_hash, status, created_at, updated_at)
SELECT ('a0000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid,
       'staff' || lpad(g::text, 2, '0') || '@seed.invalid',
       'staff' || lpad(g::text, 2, '0') || '@seed.invalid',
       '$argon2id$v=19$m=19456,t=2,p=1$/Ddh3VoIbj+zSdUC6V9zWg$sttfOk8OcSxh/MWPTSRfigeHqDBG+20Isad0jplZLYw',
       'active',
       now() - make_interval(days => 400 - g * 7),
       now() - make_interval(days => 400 - g * 7)
FROM generate_series(1, 54) AS g
ON CONFLICT DO NOTHING;

-- Companies, their memberships, and their projects are one statement so a
-- company only gets dependent rows when it is actually inserted.
WITH seed_company (id, name, slug, status, days_ago, members, ord) AS (
    VALUES
        ('c0000000-0000-4000-8000-000000000001'::uuid, 'PT Graha Nusantara Konstruksi', 'graha-nusantara-konstruksi', 'active',    402, 5,  1),
        ('c0000000-0000-4000-8000-000000000002'::uuid, 'PT Bangun Persada Utama',       'bangun-persada-utama',       'active',    361, 4,  2),
        ('c0000000-0000-4000-8000-000000000003'::uuid, 'PT Cipta Karsa Infrastruktur',  'cipta-karsa-infrastruktur',  'active',    318, 6,  3),
        ('c0000000-0000-4000-8000-000000000004'::uuid, 'PT Mitra Jalan Raya',           'mitra-jalan-raya',           'suspended', 286, 3,  4),
        ('c0000000-0000-4000-8000-000000000005'::uuid, 'PT Anugerah Struktur Baja',     'anugerah-struktur-baja',     'active',    254, 4,  5),
        ('c0000000-0000-4000-8000-000000000006'::uuid, 'PT Nusa Piling Perkasa',        'nusa-piling-perkasa',        'active',    221, 5,  6),
        ('c0000000-0000-4000-8000-000000000007'::uuid, 'PT Tirta Sarana Teknik',        'tirta-sarana-teknik',        'active',    187, 3,  7),
        ('c0000000-0000-4000-8000-000000000008'::uuid, 'PT Bumi Rancang Arsitek',       'bumi-rancang-arsitek',       'active',    152, 4,  8),
        ('c0000000-0000-4000-8000-000000000009'::uuid, 'PT Sentosa Readymix',           'sentosa-readymix',           'suspended', 118, 2,  9),
        ('c0000000-0000-4000-8000-000000000010'::uuid, 'PT Cahaya Interior Kontraktor', 'cahaya-interior-kontraktor', 'active',     96, 5, 10),
        ('c0000000-0000-4000-8000-000000000011'::uuid, 'PT Karya Tiga Putra',           'karya-tiga-putra',           'active',     71, 3, 11),
        ('c0000000-0000-4000-8000-000000000012'::uuid, 'PT Prasada Rekayasa',           'prasada-rekayasa',           'active',     44, 4, 12),
        ('c0000000-0000-4000-8000-000000000013'::uuid, 'PT Surya Beton Perkasa',        'surya-beton-perkasa',        'active',     21, 3, 13),
        ('c0000000-0000-4000-8000-000000000014'::uuid, 'PT Lima Benua Kontraktor',      'lima-benua-kontraktor',      'active',      6, 2, 14)
), inserted_company AS (
    INSERT INTO app.companies (id, name, slug, status, created_at, updated_at)
    SELECT id, name, slug, status, now() - make_interval(days => days_ago), now() - make_interval(days => days_ago)
    FROM seed_company
    ON CONFLICT DO NOTHING
    RETURNING id
), created AS (
    -- now() is the transaction timestamp, so dependent rows line up with the
    -- company timestamp written above.
    SELECT s.id, s.ord, s.members, now() - make_interval(days => s.days_ago) AS created_at
    FROM seed_company s JOIN inserted_company i ON i.id = s.id
), inserted_membership AS (
    -- One active owner per company (a partial unique index enforces this), then an
    -- admin and regular members. The last member of a five-person company is
    -- suspended so the active member count differs from the roster size.
    INSERT INTO app.company_memberships (company_id, user_id, role, status, created_at, updated_at)
    SELECT c.id,
           ('a0000000-0000-4000-8000-' || lpad(((c.ord - 1) * 4 + g)::text, 12, '0'))::uuid,
           CASE WHEN g = 1 THEN 'owner' WHEN g = 2 THEN 'admin' ELSE 'member' END,
           CASE WHEN c.members >= 5 AND g = c.members THEN 'suspended' ELSE 'active' END,
           c.created_at + interval '2 days',
           c.created_at + interval '2 days'
    FROM created c CROSS JOIN LATERAL generate_series(1, c.members) AS g
    ON CONFLICT DO NOTHING
    RETURNING company_id
), inserted_project AS (
    INSERT INTO app.projects (company_id, project_code, name, project_type, status, created_at, updated_at)
    SELECT c.id,
           'PRJ-' || lpad(c.ord::text, 2, '0') || '-' || p::text,
           'Paket Pekerjaan ' || p::text,
           CASE p WHEN 1 THEN 'gedung' ELSE 'jalan' END,
           CASE p WHEN 1 THEN 'active' ELSE 'closed' END,
           c.created_at + interval '5 days',
           c.created_at + interval '5 days'
    FROM created c CROSS JOIN generate_series(1, 2) AS p
    ON CONFLICT DO NOTHING
    RETURNING id
)
SELECT (SELECT count(*) FROM inserted_company) AS companies,
       (SELECT count(*) FROM inserted_membership) AS memberships,
       (SELECT count(*) FROM inserted_project) AS projects;

-- Queue history for the failed-process KPI and the attention table. Four
-- failures sit in the current period, two in the previous one, and one pending
-- event is retryable. The email dead letter is what makes the email integration
-- read as degraded; there is deliberately no heartbeat row, because worker
-- liveness must come from a running worker.
INSERT INTO app.outbox_events (company_id, event_type, payload, idempotency_key, status, attempts, available_at, last_failed_at, error_code, created_at, delivered_at)
VALUES
    ('c0000000-0000-4000-8000-000000000003'::uuid, 'email.delivery', '{"template":"company_invitation"}'::jsonb, 'seed-dashboard-email-1', 'dead', 5, now() - interval '2 hours', now() - interval '2 hours', 'JOB_EXECUTION_FAILED', now() - interval '3 hours', NULL),
    ('c0000000-0000-4000-8000-000000000006'::uuid, 'telegram.notification', '{"chat":"operations"}'::jsonb, 'seed-dashboard-telegram-1', 'dead', 3, now() - interval '26 hours', now() - interval '26 hours', 'JOB_EXECUTION_FAILED', now() - interval '27 hours', NULL),
    ('c0000000-0000-4000-8000-000000000010'::uuid, 'report.export', '{"format":"xlsx"}'::jsonb, 'seed-dashboard-export-1', 'dead', 4, now() - interval '4 days', now() - interval '4 days', 'JOB_EXECUTION_FAILED', now() - interval '4 days 1 hour', NULL),
    ('c0000000-0000-4000-8000-000000000012'::uuid, 'email.delivery', '{"template":"password_reset"}'::jsonb, 'seed-dashboard-email-2', 'pending', 2, now() + interval '5 minutes', now() - interval '40 minutes', 'JOB_EXECUTION_FAILED', now() - interval '50 minutes', NULL),
    ('c0000000-0000-4000-8000-000000000002'::uuid, 'document.render', '{"template":"rab"}'::jsonb, 'seed-dashboard-render-1', 'pending', 1, now() - interval '45 days', now() - interval '45 days', 'JOB_EXECUTION_FAILED', now() - interval '45 days 1 hour', NULL),
    ('c0000000-0000-4000-8000-000000000007'::uuid, 'email.delivery', '{"template":"company_invitation"}'::jsonb, 'seed-dashboard-email-3', 'dead', 5, now() - interval '52 days', now() - interval '52 days', 'JOB_EXECUTION_FAILED', now() - interval '52 days 1 hour', NULL),
    ('c0000000-0000-4000-8000-000000000005'::uuid, 'email.delivery', '{"template":"email_verification"}'::jsonb, 'seed-dashboard-email-4', 'delivered', 1, now() - interval '6 days', NULL, NULL, now() - interval '6 days 10 minutes', now() - interval '6 days'),
    ('c0000000-0000-4000-8000-000000000008'::uuid, 'document.render', '{"template":"invoice"}'::jsonb, 'seed-dashboard-render-2', 'delivered', 1, now() - interval '15 days', NULL, NULL, now() - interval '15 days 10 minutes', now() - interval '15 days')
ON CONFLICT (idempotency_key) DO NOTHING;

-- Plan catalog and current assignments. Thirteen companies hold a current
-- subscription (Business 6, Starter 3, Enterprise 2, Trial 2); the suspended
-- ready-mix company keeps only a canceled row, so its plan cell stays "—" and it
-- is excluded from the distribution widget. Production keeps its own catalog;
-- these plan definitions exist only in this demo dataset.
INSERT INTO app.plans (id, code, name) VALUES
    ('f0000000-0000-4000-8000-000000000001'::uuid, 'trial',      'Trial'),
    ('f0000000-0000-4000-8000-000000000002'::uuid, 'starter',    'Starter'),
    ('f0000000-0000-4000-8000-000000000003'::uuid, 'business',   'Business'),
    ('f0000000-0000-4000-8000-000000000004'::uuid, 'enterprise', 'Enterprise')
ON CONFLICT DO NOTHING;

INSERT INTO app.subscriptions (id, company_id, plan_id, status, period_start, period_end, created_at, updated_at)
SELECT v.id, v.company_id, p.id, v.status,
       now() - v.age,
       CASE WHEN v.status = 'canceled' THEN now() - v.age + interval '90 days'
            ELSE now() - v.age + interval '1 year' END,
       now() - v.age, now() - v.age
FROM (VALUES
    ('e0000000-0000-4000-8000-000000000001'::uuid, 'c0000000-0000-4000-8000-000000000001'::uuid, 'enterprise', 'active',   interval '402 days'),
    ('e0000000-0000-4000-8000-000000000002'::uuid, 'c0000000-0000-4000-8000-000000000002'::uuid, 'business',   'active',   interval '361 days'),
    ('e0000000-0000-4000-8000-000000000003'::uuid, 'c0000000-0000-4000-8000-000000000003'::uuid, 'enterprise', 'active',   interval '318 days'),
    ('e0000000-0000-4000-8000-000000000004'::uuid, 'c0000000-0000-4000-8000-000000000004'::uuid, 'starter',    'past_due', interval '286 days'),
    ('e0000000-0000-4000-8000-000000000005'::uuid, 'c0000000-0000-4000-8000-000000000005'::uuid, 'business',   'active',   interval '254 days'),
    ('e0000000-0000-4000-8000-000000000006'::uuid, 'c0000000-0000-4000-8000-000000000006'::uuid, 'business',   'active',   interval '221 days'),
    ('e0000000-0000-4000-8000-000000000007'::uuid, 'c0000000-0000-4000-8000-000000000007'::uuid, 'starter',    'active',   interval '187 days'),
    ('e0000000-0000-4000-8000-000000000008'::uuid, 'c0000000-0000-4000-8000-000000000008'::uuid, 'business',   'active',   interval '152 days'),
    ('e0000000-0000-4000-8000-000000000009'::uuid, 'c0000000-0000-4000-8000-000000000009'::uuid, 'enterprise', 'canceled', interval '118 days'),
    ('e0000000-0000-4000-8000-000000000010'::uuid, 'c0000000-0000-4000-8000-000000000010'::uuid, 'business',   'active',   interval '96 days'),
    ('e0000000-0000-4000-8000-000000000011'::uuid, 'c0000000-0000-4000-8000-000000000011'::uuid, 'trial',      'trialing', interval '71 days'),
    ('e0000000-0000-4000-8000-000000000012'::uuid, 'c0000000-0000-4000-8000-000000000012'::uuid, 'starter',    'active',   interval '44 days'),
    ('e0000000-0000-4000-8000-000000000013'::uuid, 'c0000000-0000-4000-8000-000000000013'::uuid, 'business',   'active',   interval '21 days'),
    ('e0000000-0000-4000-8000-000000000014'::uuid, 'c0000000-0000-4000-8000-000000000014'::uuid, 'trial',      'trialing', interval '6 days')
) AS v(id, company_id, plan_code, status, age)
JOIN app.plans p ON p.code = v.plan_code
ON CONFLICT DO NOTHING;

-- Platform actions for the activity feed. audit_logs rejects UPDATE and DELETE,
-- so the rows are guarded by a request_id marker instead of being replaceable.
INSERT INTO app.audit_logs (company_id, action, entity_type, entity_id, occurred_at, request_id, metadata_json)
SELECT v.company_id, v.action, 'company', v.company_id, v.occurred_at, 'seed:dashboard-demo', v.metadata
FROM (VALUES
    ('c0000000-0000-4000-8000-000000000009'::uuid, 'company.reactivated', now() - interval '40 days', '{}'::jsonb),
    ('c0000000-0000-4000-8000-000000000004'::uuid, 'company.suspended',   now() - interval '18 days', '{"reason":"payment_failure"}'::jsonb),
    ('c0000000-0000-4000-8000-000000000003'::uuid, 'subscription.changed', now() - interval '12 days', '{"from":"starter","to":"business"}'::jsonb),
    ('c0000000-0000-4000-8000-000000000009'::uuid, 'company.suspended',   now() - interval '9 days',  '{"reason":"policy_violation"}'::jsonb),
    ('c0000000-0000-4000-8000-000000000012'::uuid, 'subscription.changed', now() - interval '3 days',  '{"from":"trial","to":"starter"}'::jsonb)
) AS v(company_id, action, occurred_at, metadata)
WHERE NOT EXISTS (SELECT 1 FROM app.audit_logs WHERE request_id = 'seed:dashboard-demo');
