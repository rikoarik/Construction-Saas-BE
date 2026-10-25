package projects

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"construction-saas/backend/internal/database"
	"construction-saas/backend/internal/identity"
	"construction-saas/backend/internal/tenancy"
)

func setup(t *testing.T) (*Authorizer, *tenancy.Store, *identity.Store, *pgxpool.Pool) {
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
	authorizer, err := NewAuthorizer(pool)
	if err != nil {
		t.Fatal(err)
	}
	tenants, err := tenancy.NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	users, err := identity.NewStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	return authorizer, tenants, users, pool
}

func TestPermissionPolicyDefaultDenies(t *testing.T) {
	authorizer, tenants, users, pool := setup(t)
	ctx := context.Background()
	stamp := time.Now().Format("20060102150405.000000000")
	owner, err := users.CreateUser(ctx, "be014-owner-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.CreateUser(ctx, "be014-member-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := users.CreateUser(ctx, "be014-outsider-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	company, err := tenants.CreateCompany(ctx, owner.ID, "BE014", "be014-"+stamp)
	if err != nil {
		t.Fatal(err)
	}
	project, err := createProject(ctx, pool, company.ID, "project-"+stamp)
	if err != nil {
		t.Fatal(err)
	}

	policy := GrantPolicy{"supervisor": {"project.read": {}, "report.review": {}}}
	checks := []struct {
		name, userID, companyID, projectID, permission string
		policy                                         GrantPolicy
		wantErr                                        error
	}{
		{"no project membership", owner.ID, company.ID, project, "project.read", policy, ErrDenied},
		{"company membership alone", owner.ID, company.ID, project, "report.review", policy, ErrDenied},
		{"outsider tenant", outsider.ID, company.ID, project, "project.read", policy, ErrDenied},
		{"unknown permission", member.ID, company.ID, project, "billing.override", policy, ErrDenied},
		{"unknown role", member.ID, company.ID, project, "project.read", GrantPolicy{"strange-role": {"project.read": {}}}, ErrDenied},
		{"missing policy", member.ID, company.ID, project, "project.read", nil, ErrDenied},
		{"explicit grant", member.ID, company.ID, project, "project.read", policy, nil},
	}
	if _, err := tenants.Membership(ctx, company.ID, member.ID); !errors.Is(err, tenancy.ErrNotFound) {
		t.Fatalf("member setup unexpectedly has company membership: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app.company_memberships(company_id,user_id,role,status) VALUES($1::uuid,$2::uuid,'member','active')`, company.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app.project_memberships(company_id,project_id,user_id,role,status) VALUES($1::uuid,$2::uuid,$3::uuid,'supervisor','active')`, company.ID, project, member.ID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			err := authorizer.Check(ctx, tc.userID, tc.companyID, tc.projectID, tc.permission, tc.policy)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("Check error = %v, want allow", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("Check error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestInactiveCompanyOrProjectMembershipDenied(t *testing.T) {
	authorizer, tenants, users, pool := setup(t)
	ctx := context.Background()
	stamp := time.Now().Format("20060102150405.000000000")
	owner, err := users.CreateUser(ctx, "inactive-owner-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.CreateUser(ctx, "inactive-member-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	company, err := tenants.CreateCompany(ctx, owner.ID, "Inactive", "inactive-"+stamp)
	if err != nil {
		t.Fatal(err)
	}
	project, err := createProject(ctx, pool, company.ID, "inactive-project-"+stamp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app.company_memberships(company_id,user_id,role,status) VALUES($1::uuid,$2::uuid,'member','active')`, company.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO app.project_memberships(company_id,project_id,user_id,role,status) VALUES($1::uuid,$2::uuid,$3::uuid,'supervisor','active')`, company.ID, project, member.ID); err != nil {
		t.Fatal(err)
	}
	policy := GrantPolicy{"supervisor": {"project.read": {}}}
	if _, err := pool.Exec(ctx, `UPDATE app.company_memberships SET status='suspended' WHERE company_id=$1::uuid AND user_id=$2::uuid`, company.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if err := authorizer.Check(ctx, member.ID, company.ID, project, "project.read", policy); !errors.Is(err, ErrDenied) {
		t.Fatalf("suspended company membership check = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE app.company_memberships SET status='active' WHERE company_id=$1::uuid AND user_id=$2::uuid`, company.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE app.project_memberships SET status='removed' WHERE company_id=$1::uuid AND project_id=$2::uuid AND user_id=$3::uuid`, company.ID, project, member.ID); err != nil {
		t.Fatal(err)
	}
	if err := authorizer.Check(ctx, member.ID, company.ID, project, "project.read", policy); !errors.Is(err, ErrDenied) {
		t.Fatalf("removed project membership check = %v", err)
	}
}

func createProject(ctx context.Context, pool *pgxpool.Pool, companyID, code string) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `INSERT INTO app.projects(company_id,project_code,name,project_type) VALUES($1::uuid,$2,$2,'building') RETURNING id::text`, companyID, code).Scan(&id)
	return id, err
}
