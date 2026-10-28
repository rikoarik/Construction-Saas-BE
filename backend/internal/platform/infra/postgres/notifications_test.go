package postgres

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"construction-saas/backend/internal/database"
	"github.com/jackc/pgx/v5"
)

func TestNotificationFeedAndReadMarkers(t *testing.T) {
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

	now := time.Now().UTC()
	suffix := now.Format("150405.000000000")
	var operator, other, companyID, suspendedID, planID, subscriptionID string
	var deadID, retryID, deliveredID int64
	if err := tx.QueryRow(ctx, `INSERT INTO app.users (email,email_normalized,password_hash,status) VALUES ($1,$1,'hash','active') RETURNING id::text`, "notif-operator-"+suffix).Scan(&operator); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO app.users (email,email_normalized,password_hash,status) VALUES ($1,$1,'hash','active') RETURNING id::text`, "notif-other-"+suffix).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO app.companies (name,slug,status) VALUES ('Notif Active',$1,'active') RETURNING id::text`, "notif-active-"+suffix).Scan(&companyID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO app.companies (name,slug,status) VALUES ('Notif Suspended',$1,'suspended') RETURNING id::text`, "notif-suspended-"+suffix).Scan(&suspendedID); err != nil {
		t.Fatal(err)
	}
	// Plan codes only accept [a-z0-9_-], so the timestamp suffix is sanitised.
	if err := tx.QueryRow(ctx, `INSERT INTO app.plans (code,name) VALUES ($1,'Notif Plan') RETURNING id::text`, "notif-plan-"+strings.ReplaceAll(suffix, ".", "-")).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO app.subscriptions (company_id,plan_id,status) VALUES ($1,$2,'past_due') RETURNING id::text`, companyID, planID).Scan(&subscriptionID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO app.outbox_events (company_id,event_type,payload,idempotency_key,status,last_failed_at) VALUES ($1,'email.delivery','{}',$2,'dead',$3) RETURNING id`, companyID, "notif-dead-"+suffix, now.Add(-time.Minute)).Scan(&deadID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO app.outbox_events (company_id,event_type,payload,idempotency_key,status,last_failed_at) VALUES ($1,'telegram.notification','{}',$2,'pending',$3) RETURNING id`, companyID, "notif-retry-"+suffix, now.Add(-2*time.Minute)).Scan(&retryID); err != nil {
		t.Fatal(err)
	}
	// A delivered event carries a failure time from an earlier attempt but is not a
	// current problem, and a pending event that never failed is not one either.
	if err := tx.QueryRow(ctx, `INSERT INTO app.outbox_events (company_id,event_type,payload,idempotency_key,status,delivered_at,last_failed_at) VALUES ($1,'email.delivery','{}',$2,'delivered',$3,$4) RETURNING id`, companyID, "notif-delivered-"+suffix, now, now.Add(-time.Hour)).Scan(&deliveredID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.outbox_events (company_id,event_type,payload,idempotency_key,status) VALUES ($1,'email.delivery','{}',$2,'pending')`, companyID, "notif-fresh-"+suffix); err != nil {
		t.Fatal(err)
	}
	// A legacy dead letter has no recorded failure time; it is still a current
	// failure, so it must appear with its creation time instead of disappearing.
	var legacyID int64
	if err := tx.QueryRow(ctx, `INSERT INTO app.outbox_events (company_id,event_type,payload,idempotency_key,status) VALUES ($1,'email.delivery','{}',$2,'dead') RETURNING id`, companyID, "notif-legacy-"+suffix).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}

	read := DashboardRead{Pool: tx}
	page, err := read.Notifications(ctx, operator, nil, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.Items == nil {
		t.Fatal("items must be an empty array, not nil")
	}
	keys := map[string]bool{}
	for _, item := range page.Items {
		keys[item.Key] = true
		if item.Read {
			t.Fatalf("a fresh operator already has read markers: %+v", item)
		}
	}
	deadKey := "process:" + strconv.FormatInt(deadID, 10)
	for _, want := range []string{deadKey, "process:" + strconv.FormatInt(retryID, 10), "process:" + strconv.FormatInt(legacyID, 10), "company:" + suspendedID, "subscription:" + subscriptionID} {
		if !keys[want] {
			t.Fatalf("feed lacks %s: %+v", want, keys)
		}
	}
	for _, unwanted := range []string{"process:" + strconv.FormatInt(deliveredID, 10), "company:" + companyID} {
		if keys[unwanted] {
			t.Fatalf("feed contains %s, which is not a current problem", unwanted)
		}
	}
	// Newest first.
	for index := 1; index < len(page.Items); index++ {
		if page.Items[index].OccurredAt.After(page.Items[index-1].OccurredAt) {
			t.Fatalf("feed is not newest first at %d", index)
		}
	}
	if page.Unread < 4 {
		t.Fatalf("unread must cover at least the four fixtures: %d", page.Unread)
	}
	baseline := page.Unread

	// Type groups map to concrete types and never to another module's rows.
	processes, err := read.Notifications(ctx, operator, []string{"process_failed", "process_retry"}, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	seenProcesses := 0
	for _, item := range processes.Items {
		if item.Type != "process_failed" && item.Type != "process_retry" {
			t.Fatalf("process filter leaked %s", item.Type)
		}
		if item.Key == deadKey || item.Key == "process:"+strconv.FormatInt(retryID, 10) {
			seenProcesses++
		}
	}
	if seenProcesses != 2 {
		t.Fatalf("process filter lacks the fixtures: %+v", processes.Items)
	}
	if processes.Unread != baseline {
		t.Fatalf("unread must ignore the filter: %d vs %d", processes.Unread, baseline)
	}

	// Marking one entry read is idempotent.
	for i := 0; i < 2; i++ {
		if err := read.MarkNotifications(ctx, operator, []string{deadKey}, true); err != nil {
			t.Fatal(err)
		}
	}
	afterRead, err := read.Notifications(ctx, operator, nil, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if afterRead.Unread != baseline-1 {
		t.Fatalf("unread after one marker: %d want %d", afterRead.Unread, baseline-1)
	}
	for _, item := range afterRead.Items {
		if item.Key == deadKey && !item.Read {
			t.Fatal("marker not reflected in the feed")
		}
	}

	// Another operator keeps their own state.
	otherPage, err := read.Notifications(ctx, other, nil, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if otherPage.Unread != baseline {
		t.Fatalf("read markers leaked across operators: %d vs %d", otherPage.Unread, baseline)
	}

	// Read filters are complementary: the marked entry is only in the read list.
	unreadOnly, err := read.Notifications(ctx, operator, nil, "unread", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range unreadOnly.Items {
		if item.Key == deadKey {
			t.Fatal("read entry still listed as unread")
		}
	}
	readOnly, err := read.Notifications(ctx, operator, nil, "read", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(readOnly.Items) != 1 || readOnly.Items[0].Key != deadKey || !readOnly.Items[0].Read {
		t.Fatalf("read filter: %+v", readOnly.Items)
	}

	// Clearing the marker restores the badge.
	if err := read.MarkNotifications(ctx, operator, []string{deadKey}, false); err != nil {
		t.Fatal(err)
	}
	restored, err := read.Notifications(ctx, operator, nil, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Unread != baseline {
		t.Fatalf("unread after clearing: %d want %d", restored.Unread, baseline)
	}

	// Mark-all only writes markers for entries that exist.
	if err := read.MarkAllNotificationsRead(ctx, operator); err != nil {
		t.Fatal(err)
	}
	all, err := read.Notifications(ctx, operator, nil, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if all.Unread != 0 {
		t.Fatalf("unread after mark-all: %d", all.Unread)
	}
	for _, item := range all.Items {
		if !item.Read {
			t.Fatalf("mark-all left an unread entry: %+v", item)
		}
	}
	var markers int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM app.platform_notification_reads WHERE user_id=$1`, operator).Scan(&markers); err != nil {
		t.Fatal(err)
	}
	if markers != len(all.Items) {
		t.Fatalf("markers %d vs entries %d", markers, len(all.Items))
	}
}
