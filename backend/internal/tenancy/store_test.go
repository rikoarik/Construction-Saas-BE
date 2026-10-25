package tenancy

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/identity"
)

func setup(t *testing.T) (*Store, *identity.Store) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := database.Open(ctx, url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(pool.Close)
	store, err := NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	users, err := identity.NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	return store, users
}

func unique() string { return time.Now().Format("20060102150405.000000000") }

func TestCreateCompanyAndMembershipAtomic(t *testing.T) {
	store, users := setup(t)
	stamp := unique()
	owner, err := users.CreateUser(context.Background(), "company-owner-"+stamp+"@example.invalid", "$test$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	company, err := store.CreateCompany(context.Background(), owner.ID, "Tenant Test", "tenant-test-"+stamp)
	if err != nil {
		t.Fatal(err)
	}
	if company.Role != "owner" {
		t.Fatalf("role=%q want owner", company.Role)
	}
	role, err := store.Membership(context.Background(), company.ID, owner.ID)
	if err != nil || role != "owner" {
		t.Fatalf("role=%q err=%v", role, err)
	}
	var settings int
	if err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM app.company_settings WHERE company_id=$1::uuid`, company.ID).Scan(&settings); err != nil || settings != 1 {
		t.Fatalf("settings=%d err=%v", settings, err)
	}
	companies, err := store.ListCompanies(context.Background(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range companies {
		if item.ID == company.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("new company missing from member list: %+v", companies)
	}
	if _, err := store.CreateCompany(context.Background(), owner.ID, "Duplicate", "tenant-test-"+stamp); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate slug err=%v", err)
	}
	if _, err := store.CreateCompany(context.Background(), "00000000-0000-0000-0000-000000000000", "Orphan", "tenant-fail-"+stamp); err == nil {
		t.Fatal("created company for non-existent owner")
	}
	var orphan int
	if err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM app.companies WHERE slug=$1`, "tenant-fail-"+stamp).Scan(&orphan); err != nil || orphan != 0 {
		t.Fatalf("orphan companies=%d err=%v", orphan, err)
	}
}

func TestMembershipUniquenessAndCompanyScopedReads(t *testing.T) {
	store, users := setup(t)
	stamp := unique()
	owner, err := users.CreateUser(context.Background(), "scope-owner-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.CreateUser(context.Background(), "scope-member-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := users.CreateUser(context.Background(), "scope-outsider-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateCompany(context.Background(), owner.ID, "First", "scope-first-"+stamp)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateCompany(context.Background(), owner.ID, "Second", "scope-second-"+stamp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(context.Background(), `INSERT INTO app.company_memberships(company_id,user_id,role,status) VALUES($1::uuid,$2::uuid,'member','active')`, first.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(context.Background(), `INSERT INTO app.company_memberships(company_id,user_id,role,status) VALUES($1::uuid,$2::uuid,'member','active')`, first.ID, member.ID); err == nil {
		t.Fatal("duplicate membership succeeded")
	} else {
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "23505" {
			t.Fatalf("duplicate membership error=%v", err)
		}
	}
	items, err := store.ListMembers(context.Background(), first.ID, owner.ID, 50)
	if err != nil || len(items) != 2 {
		t.Fatalf("members=%v err=%v", items, err)
	}
	if _, err := store.ListMembers(context.Background(), first.ID, outsider.ID, 50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider read err=%v", err)
	}
	if _, err := store.ListMembers(context.Background(), second.ID, member.ID, 50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company read err=%v", err)
	}
}

func TestCrossTenantGuessedCompanyIDsDeniedAcrossCompanyResources(t *testing.T) {
	store, users := setup(t)
	ctx := context.Background()
	stamp := unique()
	owner, err := users.CreateUser(ctx, "be015-scope-owner-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.CreateUser(ctx, "be015-scope-member-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := users.CreateUser(ctx, "be015-scope-outsider-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	companyA, err := store.CreateCompany(ctx, owner.ID, "BE015 A", "be015-company-a-"+stamp)
	if err != nil {
		t.Fatal(err)
	}
	companyB, err := store.CreateCompany(ctx, owner.ID, "BE015 B", "be015-company-b-"+stamp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO app.company_memberships(company_id,user_id,role,status) VALUES($1::uuid,$2::uuid,'member','active')`, companyA.ID, member.ID); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, userID, companyID string
	}{
		{"outsider guessed members", outsider.ID, companyA.ID},
		{"member guessed another company members", member.ID, companyB.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.ListMembers(ctx, tc.companyID, tc.userID, 50); !errors.Is(err, ErrNotFound) {
				t.Fatalf("ListMembers error = %v, want not found", err)
			}
		})
	}

	invitation, err := store.CreateInvitation(ctx, owner.ID, companyB.ID, "be015-invite-"+stamp+"@example.invalid", "member", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if invitation.Token == "" {
		t.Fatal("owner-created invitation is missing a token")
	}
	if _, err := store.ListInvitations(ctx, member.ID, companyB.ID, 50); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-company ListInvitations error = %v, want not found", err)
	}
	memberB, err := users.CreateUser(ctx, "be015-member-b-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO app.company_memberships(company_id,user_id,role,status) VALUES($1::uuid,$2::uuid,'member','active')`, companyB.ID, memberB.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateInvitation(ctx, memberB.ID, companyB.ID, "be015-forbidden-"+stamp+"@example.invalid", "member", time.Hour); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member CreateInvitation error = %v, want forbidden", err)
	}
}

func TestInvitationTokenHashAndCompanyAuthorization(t *testing.T) {
	store, users := setup(t)
	stamp := unique()
	owner, err := users.CreateUser(context.Background(), "invite-owner-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.CreateUser(context.Background(), "invite-member-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	company, err := store.CreateCompany(context.Background(), owner.ID, "Invite Tenant", "invite-tenant-"+stamp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(context.Background(), `INSERT INTO app.company_memberships(company_id,user_id,role,status) VALUES($1::uuid,$2::uuid,'member','active')`, company.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	invitation, err := store.CreateInvitation(context.Background(), owner.ID, company.ID, "Invitee@Example.invalid", "member", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if invitation.Token == "" {
		t.Fatal("token missing")
	}
	if HashInvitationToken(invitation.Token) == invitation.Token {
		t.Fatal("raw token equals hash")
	}
	var raw int
	if err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM app.company_invitations WHERE token_hash=$1`, invitation.Token).Scan(&raw); err != nil || raw != 0 {
		t.Fatalf("plaintext token persisted count=%d err=%v", raw, err)
	}
	if _, err := store.CreateInvitation(context.Background(), member.ID, company.ID, "other@example.invalid", "member", time.Hour); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member invite error=%v", err)
	}
	if _, err := store.ListInvitations(context.Background(), member.ID, company.ID, 50); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member list invitation error=%v", err)
	}
	items, err := store.ListInvitations(context.Background(), owner.ID, company.ID, 50)
	if err != nil || len(items) != 1 || items[0].Token != "" {
		t.Fatalf("listed invitations=%+v err=%v", items, err)
	}
}
