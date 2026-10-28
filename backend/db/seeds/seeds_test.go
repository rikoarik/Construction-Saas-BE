package seeds_test

import (
	"context"
	"os"
	"testing"
	"time"

	"construction-saas/backend/db/seeds"
	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/identity"
	"construction-saas/backend/internal/platform/app"
	platformpg "construction-saas/backend/internal/platform/infra/postgres"
	"github.com/jackc/pgx/v5"
)

// TestDashboardDemoFeedsExistingDashboard keeps the demo dataset honest: it must
// stay insert-only, and every widget the platform dashboard already has must find
// rows. Everything runs inside a rolled-back transaction so a shared test
// database is never polluted.
func TestDashboardDemoFeedsExistingDashboard(t *testing.T) {
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

	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := tx.Exec(ctx, seeds.DashboardDemo); err != nil {
			t.Fatalf("apply demo dataset (attempt %d): %v", attempt, err)
		}
	}
	var demoCompanies, demoUsers, demoAudits, demoSubscriptions int64
	if err := tx.QueryRow(ctx, `SELECT
	 (SELECT count(*) FROM app.companies WHERE slug IN ('graha-nusantara-konstruksi','lima-benua-kontraktor')),
	 (SELECT count(*) FROM app.users WHERE email_normalized LIKE '%' || $1),
	 (SELECT count(*) FROM app.audit_logs WHERE request_id = $2),
	 (SELECT count(*) FROM app.subscriptions WHERE id::text LIKE 'e0000000-%')`, seeds.DemoAccountDomain, seeds.DemoRequestID).
		Scan(&demoCompanies, &demoUsers, &demoAudits, &demoSubscriptions); err != nil {
		t.Fatal(err)
	}
	if demoCompanies != 2 || demoUsers != 54 || demoAudits != 5 || demoSubscriptions != 14 {
		t.Fatalf("second run must not duplicate rows: companies=%d users=%d audits=%d subscriptions=%d", demoCompanies, demoUsers, demoAudits, demoSubscriptions)
	}

	// Seeded accounts exist to populate member counts, not to be usable.
	var hash string
	if err := tx.QueryRow(ctx, `SELECT password_hash FROM app.users WHERE email_normalized = 'staff01' || $1`, seeds.DemoAccountDomain).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if ok, err := (identity.PasswordHasher{}).Verify(hash, "seed-dashboard"); err != nil || ok {
		t.Fatalf("demo account must not authenticate: ok=%v err=%v", ok, err)
	}

	read := platformpg.DashboardRead{Pool: tx}
	now := time.Now().UTC()
	period, err := app.PeriodFor("30d", now)
	if err != nil {
		t.Fatal(err)
	}
	dashboard, err := read.Dashboard(ctx, period)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Companies.Value == nil || *dashboard.Companies.Value < 14 || dashboard.Companies.Previous == nil {
		t.Fatalf("companies KPI: %+v", dashboard.Companies)
	}
	if dashboard.Users.Value == nil || *dashboard.Users.Value < 54 {
		t.Fatalf("users KPI: %+v", dashboard.Users)
	}
	// Every seeded dead letter carries a failure time, so the count must stay
	// available instead of degrading to "unknown".
	if dashboard.FailedJobs.Value == nil || *dashboard.FailedJobs.Value < 4 || dashboard.FailedJobs.Previous == nil || *dashboard.FailedJobs.Previous < 2 {
		t.Fatalf("failed jobs KPI: %+v", dashboard.FailedJobs)
	}
	// 14 active projects and no historical status, so the value is known and the
	// comparison is honestly absent.
	if dashboard.ActiveProjects.Value == nil || *dashboard.ActiveProjects.Value < 14 || dashboard.ActiveProjects.Previous != nil {
		t.Fatalf("active projects KPI: %+v", dashboard.ActiveProjects)
	}

	status, err := read.Status(ctx)
	if err != nil || status.Total < 14 || status.Active < 12 || status.Suspended < 2 {
		t.Fatalf("status chart: %+v %v", status, err)
	}

	yearly, err := app.PeriodFor("12m", now)
	if err != nil {
		t.Fatal(err)
	}
	growth, err := read.Growth(ctx, yearly)
	if err != nil || len(growth.Points) == 0 {
		t.Fatalf("growth chart: %+v %v", growth, err)
	}
	var added int64
	for _, point := range growth.Points {
		added += point.NewCompanies
	}
	if added < 10 || growth.Points[len(growth.Points)-1].TotalCompanies < 14 {
		t.Fatalf("growth chart lacks registrations: %+v", growth)
	}

	// The recent-companies page is ordered by registration time, so a shared test
	// database can hold newer rows from other packages. Scan for the demo rows
	// instead of assuming they are on the first page.
	demoNames := map[string]bool{
		"PT Graha Nusantara Konstruksi": true, "PT Bangun Persada Utama": true, "PT Cipta Karsa Infrastruktur": true,
		"PT Mitra Jalan Raya": true, "PT Anugerah Struktur Baja": true, "PT Nusa Piling Perkasa": true,
		"PT Tirta Sarana Teknik": true, "PT Bumi Rancang Arsitek": true, "PT Sentosa Readymix": true,
		"PT Cahaya Interior Kontraktor": true, "PT Karya Tiga Putra": true, "PT Prasada Rekayasa": true,
		"PT Surya Beton Perkasa": true, "PT Lima Benua Kontraktor": true,
	}
	found := make([]app.Company, 0, len(demoNames))
	for offset := 0; offset < 1000 && len(found) < len(demoNames); offset += 100 {
		page, err := read.Companies(ctx, 100, offset)
		if err != nil {
			t.Fatalf("companies page at offset %d: %v", offset, err)
		}
		for _, company := range page.Items {
			if demoNames[company.Name] {
				found = append(found, company)
			}
		}
		if !page.HasMore {
			break
		}
	}
	if len(found) == 0 {
		t.Skip("shared test database holds more recent companies than the demo dataset")
	}
	for _, company := range found {
		if company.ProjectCount == nil || *company.ProjectCount != 2 {
			t.Fatalf("demo company lacks projects: %+v", company)
		}
		// Every demo company except the canceled one holds a current plan.
		if company.Name == "PT Sentosa Readymix" {
			if company.SubscriptionPlan != nil {
				t.Fatalf("canceled subscription must not show a plan: %+v", company)
			}
			continue
		}
		if company.SubscriptionPlan == nil {
			t.Fatalf("demo company lacks a plan: %+v", company)
		}
	}
	var maxMembers int64
	for _, company := range found {
		if company.MemberCount > maxMembers {
			maxMembers = company.MemberCount
		}
	}
	if maxMembers < 2 {
		t.Fatalf("demo companies lack member counts: %+v", found)
	}
	distribution, err := read.Distribution(ctx)
	if err != nil || !distribution.Available || len(distribution.Items) == 0 {
		t.Fatalf("subscription distribution: %+v %v", distribution, err)
	}
	var distributed int64
	for _, item := range distribution.Items {
		distributed += item.Companies
		if item.Plan == "Business" && item.Companies != 6 {
			t.Fatalf("business plan count: %+v", distribution.Items)
		}
	}
	// The canceled subscription stays out of the distribution.
	if distributed != 13 {
		t.Fatalf("distribution must count current subscriptions only: %+v", distribution.Items)
	}

	// Jobs and activities are also newest-first, so scan pages until the demo
	// events appear; other packages share this database. Only rows belonging to a
	// demo company are validated, because foreign failures may lack an error code.
	seen := map[string]bool{}
	for offset := 0; offset < 500 && (!seen["failed"] || !seen["retry_pending"]); offset += 100 {
		page, err := read.Jobs(ctx, []string{"failed", "retry_pending"}, 100, offset)
		if err != nil {
			t.Fatalf("jobs page at offset %d: %v", offset, err)
		}
		for _, job := range page.Items {
			if job.CompanyName == nil || !demoNames[*job.CompanyName] {
				continue
			}
			seen[job.Status] = true
			if job.FailedAt == nil || job.ErrorCode == nil || *job.ErrorCode != "JOB_EXECUTION_FAILED" {
				t.Fatalf("seeded job is missing operational context: %+v", job)
			}
		}
		if !page.HasMore {
			break
		}
	}
	if !seen["failed"] || !seen["retry_pending"] {
		t.Fatalf("attention jobs must cover both statuses: %+v", seen)
	}

	types := map[string]bool{}
	for offset := 0; offset < 1000 && !(types["company.suspended"] && types["subscription.changed"]); offset += 100 {
		page, err := read.Activities(ctx, 100, offset)
		if err != nil {
			t.Fatalf("activities page at offset %d: %v", offset, err)
		}
		for _, activity := range page.Items {
			types[activity.Type] = true
		}
		if !page.HasMore {
			break
		}
	}
	if !types["company.registered"] || !types["company.suspended"] || !types["subscription.changed"] || !types["job.failed"] {
		t.Fatalf("activity feed lacks expected events: %+v", types)
	}

	health, err := read.IntegrationHealth(ctx)
	if err != nil || len(health) != 4 || health[1].Status != "degraded" {
		t.Fatalf("integration health: %+v %v", health, err)
	}
}
