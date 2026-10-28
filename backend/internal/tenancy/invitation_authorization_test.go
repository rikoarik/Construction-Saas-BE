package tenancy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInvitationAuthorizationRoles(t *testing.T) {
	store, users := setup(t)
	ctx := context.Background()
	stamp := unique()
	owner, err := users.CreateUser(ctx, "invite-roles-owner-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	company, err := store.CreateCompany(ctx, owner.ID, "Roles test", "invite-roles-"+stamp)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, role, status string
		want error
	}{
		{"admin", "admin", "active", nil},
		{"member", "member", "active", ErrForbidden},
		{"viewer", "viewer", "active", ErrForbidden},
		{"suspended", "admin", "suspended", ErrNotFound},
		{"outsider", "", "", ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, err := users.CreateUser(ctx, "invite-roles-"+tc.name+"-"+stamp+"@example.invalid", "$hash", "active")
			if err != nil {
				t.Fatal(err)
			}
			if tc.role != "" {
				if _, err := store.pool.Exec(ctx, `INSERT INTO app.company_memberships(company_id,user_id,role,status) VALUES($1::uuid,$2::uuid,$3,$4)`, company.ID, user.ID, tc.role, tc.status); err != nil {
					t.Fatal(err)
				}
			}
			_, err = store.CreateInvitation(ctx, user.ID, company.ID, "invitee@example.invalid", "member", time.Hour)
			if !errors.Is(err, tc.want) {
				t.Fatalf("CreateInvitation error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestInvitationAuthorizationWaitsForMembershipRevocation(t *testing.T) {
	store, users := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stamp := unique()
	owner, err := users.CreateUser(ctx, "invite-lock-owner-"+stamp+"@example.invalid", "$hash", "active")
	if err != nil {
		t.Fatal(err)
	}
	company, err := store.CreateCompany(ctx, owner.ID, "Lock test", "invite-lock-"+stamp)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `UPDATE app.company_memberships SET status='suspended' WHERE company_id=$1::uuid AND user_id=$2::uuid`, company.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := store.CreateInvitation(ctx, owner.ID, company.ID, "invitee@example.invalid", "member", time.Hour)
		result <- err
	}()
	// A concurrent authorization must not succeed while revocation is pending.
	select {
	case err := <-result:
		t.Fatalf("invitation returned before revocation committed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrNotFound) {
		t.Fatalf("invitation after revocation: %v, want not found", err)
	}
	var count int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM app.company_invitations WHERE company_id=$1::uuid`, company.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invitations=%d err=%v", count, err)
	}
}
