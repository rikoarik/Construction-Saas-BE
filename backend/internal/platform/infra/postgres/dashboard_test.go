package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/platform/app"
	"github.com/jackc/pgx/v5"
)

func TestDashboardPostgresAggregations(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := database.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Read all fixtures through this transaction so assertions remain isolated
	// from other packages using the same PostgreSQL test database.
	// The clock is shifted forward, so rows written "now" by a concurrent package
	// can land in this period's previous window. Record the baseline and assert
	// deltas instead of absolute counts.
	now := time.Now().UTC().AddDate(0, 0, 40)
	period, err := app.PeriodFor("30d", now)
	if err != nil {
		t.Fatal(err)
	}
	previousStart := period.Start.Add(-period.End.Sub(period.Start))
	var initialCompanies, initialUsers, initialActiveProjects, initialFailed, initialPreviousFailed int64
	if err := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM app.companies),
 (SELECT count(*) FROM app.users),
 (SELECT count(*) FROM app.projects WHERE status='active'),
 (SELECT count(*) FROM app.outbox_events WHERE status IN ('dead','pending') AND last_failed_at >= $1 AND last_failed_at < $2),
 (SELECT count(*) FROM app.outbox_events WHERE status IN ('dead','pending') AND last_failed_at >= $3 AND last_failed_at < $1)`,
		period.Start, period.End, previousStart).Scan(&initialCompanies, &initialUsers, &initialActiveProjects, &initialFailed, &initialPreviousFailed); err != nil {
		t.Fatal(err)
	}
	var u1, u2, c1, c2 string
	for _, p := range []struct {
		out   *string
		email string
	}{{&u1, "dashboard-a"}, {&u2, "dashboard-b"}} {
		if err := tx.QueryRow(ctx, `INSERT INTO app.users (email,email_normalized,password_hash,status) VALUES ($1,$1,'hash','active') RETURNING id::text`, p.email+now.Format("20060102150405")).Scan(p.out); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.QueryRow(ctx, `INSERT INTO app.companies (name,slug,status,created_at) VALUES ('First',$1,'active',$2) RETURNING id::text`, "dashboard-first-20261028", now.AddDate(0, 0, -40)).Scan(&c1); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO app.companies (name,slug,status,created_at) VALUES ('Second',$1,'suspended',$2) RETURNING id::text`, "dashboard-second-20261028", now.AddDate(0, 0, -2)).Scan(&c2); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ company, user string }{{c1, u1}, {c2, u1}, {c2, u2}} {
		if _, err := tx.Exec(ctx, `INSERT INTO app.company_memberships (company_id,user_id,role) VALUES ($1,$2,'member')`, v.company, v.user); err != nil {
			t.Fatal(err)
		}
	}
	// One active and one closed project for c1, one active project for c2. The KPI
	// counts active projects only; the company row counts every project.
	if _, err := tx.Exec(ctx, `INSERT INTO app.projects (company_id,project_code,name,project_type,status) VALUES
 ($1,'PRJ-DASH-1','Active job','gedung','active'), ($1,'PRJ-DASH-2','Closed job','jalan','closed'), ($2,'PRJ-DASH-3','Second active job','gedung','active')`, c1, c2); err != nil {
		t.Fatal(err)
	}
	var planID string
	if err := tx.QueryRow(ctx, `INSERT INTO app.plans (code,name) VALUES ('dashboard-test-plan','Dashboard Test') ON CONFLICT (code) DO UPDATE SET name=EXCLUDED.name RETURNING id::text`).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.subscriptions (company_id,plan_id,status) VALUES ($1,$2,'active')`, c2, planID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.outbox_events (company_id,event_type,payload,idempotency_key,status,last_failed_at) VALUES ($1,'email.delivery','{}',$2,'dead',$3), ($1,'email.delivery','{}',$4,'pending',$5), ($1,'email.delivery','{}',$6,'dead',$7)`, c1, "dashboard-1-20261028", now.Add(-time.Hour), "dashboard-2-20261028", now.Add(-2*time.Hour), "dashboard-3-20261028", now.AddDate(0, 0, -40)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.outbox_events (company_id,event_type,payload,idempotency_key,status,delivered_at,last_failed_at) VALUES ($1,'email.delivery','{}',$2,'delivered',$3,$4)`, c1, "dashboard-recovered-20261028", now.Add(-time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	read := DashboardRead{Pool: tx}
	p := period
	d, err := read.Dashboard(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if *d.Companies.Value != initialCompanies+2 || *d.Users.Value != initialUsers+2 || d.ActiveProjects.Value == nil || *d.ActiveProjects.Value != initialActiveProjects+2 || d.ActiveProjects.Previous != nil || *d.FailedJobs.Value != initialFailed+2 || d.Companies.Previous == nil || d.Users.Previous == nil || d.FailedJobs.Previous == nil || *d.FailedJobs.Previous != initialPreviousFailed+1 || *d.Companies.Previous != initialCompanies+1 || *d.Users.Previous != initialUsers+2 {
		t.Fatalf("dashboard counts or comparisons: companies=%d previous=%d users=%d previous=%d activeProjects=%v failed=%d previous=%d initial=%d/%d/%d failed=%d/%d", *d.Companies.Value, *d.Companies.Previous, *d.Users.Value, *d.Users.Previous, d.ActiveProjects, *d.FailedJobs.Value, *d.FailedJobs.Previous, initialCompanies, initialUsers, initialActiveProjects, initialFailed, initialPreviousFailed)
	}
	status, err := read.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Active+status.Suspended+status.Archived != status.Total || status.Total != initialCompanies+2 {
		t.Fatalf("status: %+v", status)
	}
	withPercentages, err := (app.Service{Read: read}).Status(ctx)
	if err != nil || withPercentages.ActivePercent != 100*float64(status.Active)/float64(status.Total) ||
		withPercentages.SuspendedPercent != 100*float64(status.Suspended)/float64(status.Total) ||
		withPercentages.ArchivedPercent != 0 {
		t.Fatalf("status percentages: %+v %v", withPercentages, err)
	}
	growth, err := read.Growth(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	var additions int64
	for _, point := range growth.Points {
		additions += point.NewCompanies
	}
	if additions != 1 || growth.Points[len(growth.Points)-1].TotalCompanies != initialCompanies+2 {
		t.Fatalf("growth: %+v", growth)
	}
	for _, point := range growth.Points {
		if point.Start.Location() != time.UTC {
			t.Fatalf("growth bucket not UTC: %+v", point)
		}
	}
	// KPI trend lines share the growth buckets: cumulative totals for companies
	// and users, failures per bucket. The project card has no line because project
	// status history does not exist.
	if len(d.Companies.Series) != len(growth.Points) || len(d.Users.Series) != len(growth.Points) || len(d.FailedJobs.Series) != len(growth.Points) {
		t.Fatalf("series must match the growth buckets: companies=%d users=%d failed=%d buckets=%d", len(d.Companies.Series), len(d.Users.Series), len(d.FailedJobs.Series), len(growth.Points))
	}
	if d.ActiveProjects.Series != nil {
		t.Fatalf("active projects cannot have a trend line: %+v", d.ActiveProjects.Series)
	}
	if last := d.Companies.Series[len(d.Companies.Series)-1]; last != *d.Companies.Value {
		t.Fatalf("company series must end at the current total: %d vs %d", last, *d.Companies.Value)
	}
	if last := d.Users.Series[len(d.Users.Series)-1]; last != *d.Users.Value {
		t.Fatalf("user series must end at the current total: %d vs %d", last, *d.Users.Value)
	}
	var seriesFailures int64
	for index, point := range growth.Points {
		seriesFailures += d.FailedJobs.Series[index]
		if index > 0 && d.Companies.Series[index] < d.Companies.Series[index-1] {
			t.Fatalf("cumulative company series decreased at %d: %+v", index, d.Companies.Series)
		}
		if point.NewCompanies > 0 && d.Companies.Series[index] == d.Companies.Series[index-1] {
			t.Fatalf("company series ignored a registration at %d", index)
		}
	}
	if seriesFailures != *d.FailedJobs.Value {
		t.Fatalf("failure series must sum to the period count: %d vs %d", seriesFailures, *d.FailedJobs.Value)
	}
	monthly, err := app.PeriodFor("12m", now)
	if err != nil {
		t.Fatal(err)
	}
	monthlyGrowth, err := read.Growth(ctx, monthly)
	if err != nil || len(monthlyGrowth.Points) == 0 || len(monthlyGrowth.Points) > 13 {
		t.Fatalf("monthly growth: %+v %v", monthlyGrowth, err)
	}
	companies, err := read.Companies(ctx, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !companies.HasMore || companies.Items[0].ID != c2 || companies.Items[0].MemberCount != 2 || companies.Items[0].ProjectCount == nil || *companies.Items[0].ProjectCount != 1 || companies.Items[0].SubscriptionPlan == nil || *companies.Items[0].SubscriptionPlan != "Dashboard Test" {
		t.Fatalf("companies: %+v", companies)
	}
	var members int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM app.company_memberships WHERE company_id=$1 AND status='active'`, c2).Scan(&members); err != nil || members != 2 {
		t.Fatalf("companies: %+v", companies)
	}
	secondPage, err := read.Companies(ctx, 1, 1)
	if err != nil || len(secondPage.Items) != 1 || secondPage.Items[0].ID != c1 || secondPage.Items[0].ProjectCount == nil || *secondPage.Items[0].ProjectCount != 2 || secondPage.Items[0].SubscriptionPlan != nil {
		t.Fatalf("company pagination: %+v %v", secondPage, err)
	}
	distribution, err := read.Distribution(ctx)
	if err != nil || !distribution.Available {
		t.Fatalf("distribution: %+v %v", distribution, err)
	}
	assigned := false
	for _, item := range distribution.Items {
		if item.Plan == "Dashboard Test" && item.Companies >= 1 {
			assigned = true
		}
	}
	if !assigned {
		t.Fatalf("distribution lacks the assigned plan: %+v", distribution.Items)
	}
	// A canceled subscription is history, not a current assignment.
	if _, err := tx.Exec(ctx, `UPDATE app.subscriptions SET status='canceled' WHERE company_id=$1`, c2); err != nil {
		t.Fatal(err)
	}
	distribution, err = read.Distribution(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range distribution.Items {
		if item.Plan == "Dashboard Test" {
			t.Fatalf("canceled subscription still counted: %+v", distribution.Items)
		}
	}
	jobs, err := read.Jobs(ctx, []string{"failed", "retry_pending"}, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs.Items) != 1 || !jobs.HasMore || jobs.Items[0].ErrorCode != nil || jobs.Items[0].RetryEligible {
		t.Fatalf("jobs: %+v", jobs)
	}
	jobPage2, err := read.Jobs(ctx, []string{"failed", "retry_pending"}, 1, 1)
	if err != nil || len(jobPage2.Items) != 1 || jobPage2.Items[0].ID == jobs.Items[0].ID {
		t.Fatalf("job pagination: %+v %v", jobPage2, err)
	}
	// A legacy dead letter remains visible but has no defensible failure period.
	if _, err := tx.Exec(ctx, `INSERT INTO app.outbox_events (company_id,event_type,payload,idempotency_key,status) VALUES ($1,'email.delivery','{}',$2,'dead')`, c1, "dashboard-legacy-20261028"); err != nil {
		t.Fatal(err)
	}
	withLegacy, err := read.Dashboard(ctx, p)
	if err != nil || withLegacy.FailedJobs.Value != nil || withLegacy.FailedJobs.Previous != nil {
		t.Fatalf("undated failure must make period count unavailable: %+v %v", withLegacy, err)
	}
	legacyJobs, err := read.Jobs(ctx, []string{"failed"}, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundLegacy := false
	for _, job := range legacyJobs.Items {
		if job.FailedAt == nil {
			foundLegacy = true
			if job.Status != "failed" || job.ErrorCode != nil {
				t.Fatalf("legacy job: %+v", job)
			}
		}
	}
	if !foundLegacy {
		t.Fatal("legacy dead letter missing from jobs")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM app.outbox_events WHERE idempotency_key=$1`, "dashboard-legacy-20261028"); err != nil {
		t.Fatal(err)
	}
	// A database without a failure in this UTC period returns a confirmed zero.
	past, err := app.PeriodFor("7d", now.AddDate(-1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	zero, err := read.Dashboard(ctx, past)
	if err != nil || zero.FailedJobs.Value == nil || *zero.FailedJobs.Value != 0 {
		t.Fatalf("expected a confirmed zero: %+v %v", zero, err)
	}
	activities, err := read.Activities(ctx, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(activities.Items) == 0 {
		t.Fatal("missing activities")
	}
	health, err := read.IntegrationHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if health[0].Status != "unknown" || health[1].Status != "degraded" || health[3].Status != "unknown" {
		t.Fatalf("health: %+v", health)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.worker_heartbeats (worker_id,event_type,checked_at) VALUES ($1,'email.delivery',now())`, "dashboard-worker-test"); err != nil {
		t.Fatal(err)
	}
	health, err = read.IntegrationHealth(ctx)
	if err != nil || health[3].Status != "healthy" || health[3].CheckedAt == nil {
		t.Fatalf("live worker heartbeat: %+v %v", health, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE app.worker_heartbeats SET checked_at=now()-interval '2 minutes' WHERE worker_id=$1`, "dashboard-worker-test"); err != nil {
		t.Fatal(err)
	}
	health, err = read.IntegrationHealth(ctx)
	if err != nil || health[3].Status != "unknown" || health[3].CheckedAt == nil {
		t.Fatalf("stale worker heartbeat: %+v %v", health, err)
	}
}

func TestStoragePostgresReportsRecordedRequests(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if err := database.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	// The counters are read through a transaction that is rolled back, so the
	// fixtures never leak into the shared table.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	read := DashboardRead{Pool: tx}

	if _, err := tx.Exec(ctx, `DELETE FROM app.api_request_daily`); err != nil {
		t.Fatal(err)
	}
	empty, err := read.Storage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Storage accounting has no authoritative source; unmetered traffic is
	// unavailable rather than a confirmed zero.
	if empty.QuotaState != "unknown" || empty.UsedBytes != nil || empty.QuotaBytes != nil || empty.APIRequests != nil {
		t.Fatalf("empty storage: %+v", empty)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO app.api_request_daily (day, requests)
		VALUES ((date_trunc('month', now() AT TIME ZONE 'UTC') - interval '1 day')::date, 5),
		       ((now() AT TIME ZONE 'UTC')::date, 7)`); err != nil {
		t.Fatal(err)
	}
	got, err := read.Storage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIRequests == nil || got.APIRequests.Used == nil || *got.APIRequests.Used != 7 || got.APIRequests.Limit != nil {
		t.Fatalf("month-to-date usage: %+v", got.APIRequests)
	}
}
