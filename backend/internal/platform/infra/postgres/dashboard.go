package postgres

import (
	"context"
	"fmt"
	"time"

	"construction-saas/backend/internal/platform/app"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DashboardRead is a cross-module operational read projection. Queries expose only
// aggregate counts and whitelisted metadata, never tenant payloads or credentials.
type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type DashboardRead struct{ Pool querier }

func (r DashboardRead) Dashboard(ctx context.Context, p app.Period) (app.Dashboard, error) {
	d := app.Dashboard{Period: p, Companies: app.Metric{}, Users: app.Metric{}, ActiveProjects: app.Metric{}, FailedJobs: app.Metric{}}
	var companies, users, previousCompanies, previousUsers, failed, previousFailed, undatedFailures, activeProjects int64
	// Separate scalar subqueries prevent multiplying users by their memberships.
	// Registration timestamps support historical totals at the period boundary.
	// Legacy dead letters lack failure times. They may belong to either period;
	// never present their counts as complete while they exist.
	previousStart := p.Start.Add(-p.End.Sub(p.Start))
	if p.Range == "12m" {
		previousStart = p.Start.AddDate(-1, 0, 0)
	}
	err := r.Pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM app.companies WHERE created_at < $2),
 (SELECT count(*) FROM app.users WHERE created_at < $2),
 (SELECT count(*) FROM app.companies WHERE created_at < $1),
 (SELECT count(*) FROM app.users WHERE created_at < $1),
 (SELECT count(*) FROM app.outbox_events WHERE status IN ('dead','pending') AND last_failed_at >= $1 AND last_failed_at < $2),
 (SELECT count(*) FROM app.outbox_events WHERE status IN ('dead','pending') AND last_failed_at >= $3 AND last_failed_at < $1),
 (SELECT count(*) FROM app.outbox_events WHERE status = 'dead' AND last_failed_at IS NULL),
 (SELECT count(*) FROM app.projects WHERE status = 'active')
 `, p.Start, p.End, previousStart).Scan(&companies, &users, &previousCompanies, &previousUsers, &failed, &previousFailed, &undatedFailures, &activeProjects)
	if err != nil {
		return d, fmt.Errorf("dashboard counts: %w", err)
	}
	d.Companies.Value, d.Users.Value = &companies, &users
	d.Companies.Previous, d.Users.Previous = &previousCompanies, &previousUsers
	if undatedFailures == 0 {
		d.FailedJobs.Value, d.FailedJobs.Previous = &failed, &previousFailed
	}
	// Projects carry a current status only; no history of status transitions is
	// recorded, so neither the prior-period count nor a trend line can be
	// reconstructed. Both stay unavailable instead of being guessed.
	d.ActiveProjects.Value = &activeProjects

	companiesSeries, usersSeries, failedSeries, err := r.series(ctx, p)
	if err != nil {
		return d, err
	}
	d.Companies.Series, d.Users.Series = companiesSeries, usersSeries
	// Undated legacy failures would make the trend incomplete in the same way
	// they make the period count unavailable.
	if undatedFailures == 0 {
		d.FailedJobs.Series = failedSeries
	}
	return d, nil
}

// series returns cumulative company and user totals plus per-bucket failures, one
// point per UTC bucket of the period. Bucket boundaries match Growth so both
// widgets describe the same calendar.
func (r DashboardRead) series(ctx context.Context, p app.Period) ([]int64, []int64, []int64, error) {
	unit := "day"
	if p.Range == "12m" {
		unit = "month"
	}
	rows, err := r.Pool.Query(ctx, `WITH buckets AS (
 SELECT generate_series(
   date_trunc($3, $1::timestamptz AT TIME ZONE 'UTC'),
   date_trunc($3, ($2::timestamptz - interval '1 microsecond') AT TIME ZONE 'UTC'),
   CASE WHEN $3 = 'month' THEN interval '1 month' ELSE interval '1 day' END
 ) AT TIME ZONE 'UTC' AS bucket
 ), company_registrations AS (
 SELECT date_trunc($3, created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket, count(*) AS n
 FROM app.companies WHERE created_at >= $1 AND created_at < $2 GROUP BY 1
 ), user_registrations AS (
 SELECT date_trunc($3, created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket, count(*) AS n
 FROM app.users WHERE created_at >= $1 AND created_at < $2 GROUP BY 1
 ), failures AS (
 SELECT date_trunc($3, last_failed_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket, count(*) AS n
 FROM app.outbox_events
 WHERE status IN ('dead','pending') AND last_failed_at >= $1 AND last_failed_at < $2 GROUP BY 1
 ), baseline AS (
 SELECT (SELECT count(*) FROM app.companies WHERE created_at < $1) AS companies,
        (SELECT count(*) FROM app.users WHERE created_at < $1) AS users
 )
 SELECT b.bucket,
 baseline.companies + sum(coalesce(cr.n, 0)) OVER (ORDER BY b.bucket),
 baseline.users + sum(coalesce(ur.n, 0)) OVER (ORDER BY b.bucket),
 coalesce(f.n, 0)
 FROM buckets b CROSS JOIN baseline
 LEFT JOIN company_registrations cr USING (bucket)
 LEFT JOIN user_registrations ur USING (bucket)
 LEFT JOIN failures f USING (bucket)
 ORDER BY b.bucket`, p.Start, p.End, unit)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("dashboard series: %w", err)
	}
	defer rows.Close()
	companies, users, failed := make([]int64, 0), make([]int64, 0), make([]int64, 0)
	for rows.Next() {
		var bucket time.Time
		var companyTotal, userTotal, failureCount int64
		if err := rows.Scan(&bucket, &companyTotal, &userTotal, &failureCount); err != nil {
			return nil, nil, nil, err
		}
		companies = append(companies, companyTotal)
		users = append(users, userTotal)
		failed = append(failed, failureCount)
	}
	return companies, users, failed, rows.Err()
}

func (r DashboardRead) Growth(ctx context.Context, p app.Period) (app.Growth, error) {
	g := app.Growth{Period: p, Points: make([]app.GrowthPoint, 0)}
	// UTC calendar buckets; the first bucket can be partial. Cumulative includes all
	// registrations strictly before each bucket's end, including pre-period history.
	unit := "day"
	if p.Range == "12m" {
		unit = "month"
	}
	rows, err := r.Pool.Query(ctx, `WITH buckets AS (
 SELECT generate_series(
   date_trunc($3, $1::timestamptz AT TIME ZONE 'UTC'),
   date_trunc($3, ($2::timestamptz - interval '1 microsecond') AT TIME ZONE 'UTC'),
   CASE WHEN $3 = 'month' THEN interval '1 month' ELSE interval '1 day' END
 ) AT TIME ZONE 'UTC' AS bucket
 ), registrations AS (
 SELECT date_trunc($3, created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket, count(*) AS n
 FROM app.companies WHERE created_at >= $1 AND created_at < $2 GROUP BY 1
 ), baseline AS (SELECT count(*) AS n FROM app.companies WHERE created_at < $1)
 SELECT b.bucket, coalesce(registrations.n, 0),
 baseline.n + sum(coalesce(registrations.n, 0)) OVER (ORDER BY b.bucket)
 FROM buckets b CROSS JOIN baseline LEFT JOIN registrations USING (bucket)
 ORDER BY b.bucket`, p.Start, p.End, unit)
	if err != nil {
		return g, fmt.Errorf("company growth: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var point app.GrowthPoint
		if err := rows.Scan(&point.Start, &point.NewCompanies, &point.TotalCompanies); err != nil {
			return g, err
		}
		point.Start = point.Start.UTC()
		g.Points = append(g.Points, point)
	}
	return g, rows.Err()
}

func (r DashboardRead) Status(ctx context.Context) (app.Status, error) {
	var s app.Status
	err := r.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'active'), count(*) FILTER (WHERE status = 'suspended'), count(*) FILTER (WHERE status = 'archived'), count(*) FROM app.companies`).Scan(&s.Active, &s.Suspended, &s.Archived, &s.Total)
	return s, err
}

func (r DashboardRead) Companies(ctx context.Context, limit, offset int) (app.Page[app.Company], error) {
	page := app.Page[app.Company]{Items: make([]app.Company, 0), Limit: limit, Offset: offset}
	// project_count counts every project of the company, including closed ones, so
	// it matches the "Projects" column rather than the active-project KPI.
	// subscription_plan stays null when no current subscription exists.
	rows, err := r.Pool.Query(ctx, `SELECT c.id::text, c.name, c.status, c.created_at,
 (SELECT pl.name FROM app.subscriptions s JOIN app.plans pl ON pl.id = s.plan_id
   WHERE s.company_id = c.id AND s.status IN ('trialing','active','past_due')
   LIMIT 1),
 (SELECT count(*) FROM app.company_memberships m WHERE m.company_id = c.id AND m.status = 'active'),
 (SELECT count(*) FROM app.projects pj WHERE pj.company_id = c.id)
 FROM app.companies c ORDER BY c.created_at DESC, c.id DESC LIMIT $1 OFFSET $2`, limit+1, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var c app.Company
		if err := rows.Scan(&c.ID, &c.Name, &c.Status, &c.CreatedAt, &c.SubscriptionPlan, &c.MemberCount, &c.ProjectCount); err != nil {
			return page, err
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, c)
	}
	return page, rows.Err()
}

func (r DashboardRead) Jobs(ctx context.Context, statuses []string, limit, offset int) (app.Page[app.Job], error) {
	page := app.Page[app.Job]{Items: make([]app.Job, 0), Limit: limit, Offset: offset}
	// Outbox 'dead' is failed; 'pending' with a recorded failure is retry_pending.
	// Legacy dead letters may have no failure time. The worker stores only a
	// generic failure marker: never return last_error or payload.
	rows, err := r.Pool.Query(ctx, `SELECT e.id, e.company_id::text, c.name, e.event_type,
 CASE WHEN e.status = 'dead' THEN 'failed' ELSE 'retry_pending' END, e.created_at,
 e.last_failed_at, e.available_at, e.attempts, e.error_code
 FROM app.outbox_events e LEFT JOIN app.companies c ON c.id = e.company_id
 WHERE (e.status = 'dead' AND 'failed' = ANY($1::text[])
 OR e.status = 'pending' AND e.last_failed_at IS NOT NULL AND 'retry_pending' = ANY($1::text[]))
 ORDER BY e.last_failed_at DESC NULLS LAST, e.created_at DESC, e.id DESC LIMIT $2 OFFSET $3`, statuses, limit+1, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var j app.Job
		if err := rows.Scan(&j.ID, &j.CompanyID, &j.CompanyName, &j.Type, &j.Status, &j.CreatedAt, &j.FailedAt, &j.AvailableAt, &j.Attempts, &j.ErrorCode); err != nil {
			return page, err
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, j)
	}
	return page, rows.Err()
}

func (r DashboardRead) Activities(ctx context.Context, limit, offset int) (app.Page[app.Activity], error) {
	page := app.Page[app.Activity]{Items: make([]app.Activity, 0), Limit: limit, Offset: offset}
	// Explicit allowlist: audit metadata can contain private tenant information.
	rows, err := r.Pool.Query(ctx, `SELECT id, action, company_id::text, occurred_at FROM (
 SELECT id::text AS id, action, company_id, occurred_at, id AS sort_id
 FROM app.audit_logs WHERE action IN ('company.suspended','company.reactivated','subscription.changed')
 UNION ALL
 SELECT 'registration:' || id::text, 'company.registered', id, created_at, 0::bigint FROM app.companies
 UNION ALL
 SELECT 'job:' || id::text, 'job.failed', company_id, last_failed_at, id FROM app.outbox_events
 WHERE last_failed_at IS NOT NULL
 ) activity ORDER BY occurred_at DESC, id DESC LIMIT $1 OFFSET $2`, limit+1, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var a app.Activity
		if err := rows.Scan(&a.ID, &a.Type, &a.CompanyID, &a.OccurredAt); err != nil {
			return page, err
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, a)
	}
	return page, rows.Err()
}

func (r DashboardRead) Distribution(ctx context.Context) (app.Distribution, error) {
	result := app.Distribution{Items: make([]app.PlanCount, 0), Available: true}
	// Current subscriptions only: canceled rows are history, not an assignment.
	// Plan names are operator-facing labels; codes stay stable for joins.
	rows, err := r.Pool.Query(ctx, `SELECT pl.name, count(*)
 FROM app.subscriptions s JOIN app.plans pl ON pl.id = s.plan_id
 WHERE s.status IN ('trialing','active','past_due')
 GROUP BY pl.id, pl.name, pl.code ORDER BY count(*) DESC, pl.code`)
	if err != nil {
		return result, fmt.Errorf("subscription distribution: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item app.PlanCount
		if err := rows.Scan(&item.Plan, &item.Companies); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func (r DashboardRead) IntegrationHealth(ctx context.Context) ([]app.Integration, error) {
	result := []app.Integration{
		{Name: "telegram", Status: "unknown"}, {Name: "email", Status: "unknown"},
		{Name: "object_storage", Status: "unknown"}, {Name: "worker", Status: "unknown"},
	}
	// An email failure is evidence of delivery degradation, including legacy dead
	// letters, but does not establish worker liveness or implicate the worker.
	var failed bool
	if err := r.Pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM app.outbox_events WHERE event_type = 'email.delivery'
		AND (status = 'dead' OR status = 'pending' AND last_failed_at IS NOT NULL)
	)`).Scan(&failed); err != nil {
		return nil, fmt.Errorf("email delivery health: %w", err)
	}
	if failed {
		result[1].Status = "degraded"
	}
	// A recent heartbeat follows a successful queue poll: evidence of process
	// liveness, not SMTP availability or successful delivery. A stale heartbeat
	// is unknown because the worker may have been intentionally stopped.
	var checkedAt *time.Time
	if err := r.Pool.QueryRow(ctx, `SELECT max(checked_at) FROM app.worker_heartbeats WHERE event_type='email.delivery'`).Scan(&checkedAt); err != nil {
		return nil, fmt.Errorf("worker heartbeat health: %w", err)
	}
	if checkedAt != nil {
		observed := checkedAt.UTC()
		result[3].CheckedAt = &observed
		age := time.Since(observed)
		if age >= 0 && age < 30*time.Second {
			result[3].Status = "healthy"
		}
	}
	return result, nil
}

// Storage reports what the platform can actually measure. Storage accounting
// and quotas have no authoritative source, so those fields stay null and
// unknown; API request counts do have one, in app.api_request_daily.
func (r DashboardRead) Storage(ctx context.Context) (app.Storage, error) {
	var metered bool
	var used int64
	// The month window is the UTC calendar month, which matches the counter's
	// day keys. An empty table means metering never ran: unavailable, not zero.
	if err := r.Pool.QueryRow(ctx, `SELECT
 EXISTS (SELECT 1 FROM app.api_request_daily),
 coalesce((SELECT sum(requests)::bigint FROM app.api_request_daily
   WHERE day >= date_trunc('month', (now() AT TIME ZONE 'UTC')::date)), 0)
 `).Scan(&metered, &used); err != nil {
		return app.Storage{}, fmt.Errorf("storage usage: %w", err)
	}
	storage := app.Storage{QuotaState: "unknown"}
	if metered {
		storage.APIRequests = &app.APIRequestUsage{Used: &used}
	}
	return storage, nil
}

var _ app.Repository = DashboardRead{}
