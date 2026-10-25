package audit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"construction-saas/backend/internal/database"
)

func TestAppendValidatesEventBeforeDatabaseCall(t *testing.T) {
	valid := Event{CompanyID: stringPtr("00000000-0000-0000-0000-000000000001"), Action: "project.created", EntityType: "project", Metadata: json.RawMessage(`{}`)}
	for _, event := range []Event{
		{},
		{Action: valid.Action, EntityType: valid.EntityType, Metadata: valid.Metadata},
		{CompanyID: valid.CompanyID, EntityType: valid.EntityType, Metadata: valid.Metadata},
		{CompanyID: valid.CompanyID, Action: valid.Action, Metadata: valid.Metadata},
		{CompanyID: valid.CompanyID, Action: " padded ", EntityType: valid.EntityType},
		{CompanyID: valid.CompanyID, Action: valid.Action, EntityType: valid.EntityType, Metadata: json.RawMessage(`[]`)},
		{CompanyID: valid.CompanyID, Action: valid.Action, EntityType: valid.EntityType, Metadata: json.RawMessage(`null`)},
		{CompanyID: valid.CompanyID, Action: valid.Action, EntityType: valid.EntityType, Metadata: json.RawMessage(`invalid`)},
	} {
		if err := Append(context.Background(), nil, event); !errors.Is(err, ErrInvalid) {
			t.Errorf("Append(%+v) error = %v, want ErrInvalid", event, err)
		}
	}
}

func TestAppendRequiresTransaction(t *testing.T) {
	event := Event{CompanyID: stringPtr("00000000-0000-0000-0000-000000000001"), Action: "project.created", EntityType: "project"}
	if err := Append(context.Background(), nil, event); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Append with nil tx error = %v, want ErrInvalid", err)
	}
}

func TestAppendPostgresAndAppendOnly(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pool.Close()

	companyID := "00000000-0000-0000-0000-000000000001"
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := Append(ctx, tx, Event{CompanyID: &companyID, Action: "project.created", EntityType: "project", RequestID: "req-1", Metadata: json.RawMessage(`{"source":"test"}`)}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("append: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM app.audit_logs WHERE company_id=$1 AND action='project.created' AND request_id='req-1' ORDER BY id DESC LIMIT 1`, companyID).Scan(&id); err != nil {
		t.Fatalf("read audit event: %v", err)
	}
	for _, query := range []string{
		`UPDATE app.audit_logs SET action='tampered' WHERE id=$1`,
		`DELETE FROM app.audit_logs WHERE id=$1`,
	} {
		if _, err := pool.Exec(ctx, query, id); err == nil {
			t.Errorf("mutation query %q succeeded", query)
		}
	}
	var action string
	if err := pool.QueryRow(ctx, `SELECT action FROM app.audit_logs WHERE id=$1`, id).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != "project.created" {
		t.Fatalf("audit action = %q, want immutable original", action)
	}

	rollbackKey := "req-rollback"
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := Append(ctx, tx, Event{CompanyID: &companyID, Action: "project.updated", EntityType: "project", RequestID: rollbackKey}); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("append rollback fixture: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM app.audit_logs WHERE company_id=$1 AND request_id=$2`, companyID, rollbackKey).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back audit rows = %d, want 0", count)
	}
}

func stringPtr(value string) *string { return &value }
