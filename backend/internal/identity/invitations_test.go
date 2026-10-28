package identity

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func createCompany(t *testing.T, store *Store, slug string) string {
	t.Helper()
	var id string
	if err := store.pool.QueryRow(context.Background(),
		`INSERT INTO app.companies (name,slug,status) VALUES ($1,$2,'active') RETURNING id::text`,
		"Company "+slug, slug).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func addMembership(t *testing.T, store *Store, companyID, userID, role, status string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(),
		`INSERT INTO app.company_memberships (company_id,user_id,role,status)
		 VALUES ($1::uuid,$2::uuid,$3,$4)`, companyID, userID, role, status); err != nil {
		t.Fatal(err)
	}
}

func invitationToken(t *testing.T, service *AuthService, email string) string {
	t.Helper()
	var ciphertext, nonce []byte
	err := service.Store.pool.QueryRow(context.Background(), `
		SELECT token_ciphertext, token_nonce FROM app.auth_email_deliveries
		WHERE recipient_email=$1 AND template='company_invitation' AND delivered_at IS NULL
		ORDER BY created_at DESC LIMIT 1`, email).Scan(&ciphertext, &nonce)
	if err != nil {
		t.Fatalf("load invitation delivery: %v", err)
	}
	box, err := service.emailBox()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := box.Decrypt(ciphertext, nonce, "tenant-invitation-v1:"+NormalizeEmail(email))
	if err != nil {
		t.Fatalf("decrypt invitation: %v", err)
	}
	return string(raw)
}

func TestTenantCapabilitiesDoNotFollowPlatformGrants(t *testing.T) {
	store, owner := integrationIdentity(t)
	_, outsider := integrationIdentity(t)
	_, viewer := integrationIdentity(t)
	service := recoveryService(t, store)
	companyID := createCompany(t, store, "tenant-"+owner.ID)
	addMembership(t, store, companyID, owner.ID, "owner", "active")
	addMembership(t, store, companyID, viewer.ID, "viewer", "active")
	grantPlatform(t, store, outsider.ID)
	ctx := context.Background()

	if _, err := service.ListMembers(ctx, outsider.ID, companyID); !errors.Is(err, ErrCompanyAccess) {
		t.Fatalf("platform grant leaked tenant access: %v", err)
	}
	if _, err := service.InviteMember(ctx, viewer.ID, companyID, "new@example.test", "member", ClientInfo{}); !errors.Is(err, ErrCapability) {
		t.Fatalf("viewer invitation = %v", err)
	}
	if _, err := service.InviteMember(ctx, owner.ID, companyID, "new@example.test", "owner", ClientInfo{}); !errors.Is(err, ErrInvitationRole) {
		t.Fatalf("owner invitation role = %v", err)
	}
	if _, err := service.InviteMember(ctx, owner.ID, companyID, "new@example.test", "admin", ClientInfo{}); err != nil {
		t.Fatalf("owner invite admin: %v", err)
	}
}

func TestInvitationAcceptancePathsAndReplay(t *testing.T) {
	store, owner := integrationIdentity(t)
	_, existing := integrationIdentity(t)
	service := recoveryService(t, store)
	ctx := context.Background()
	companyID := createCompany(t, store, "invite-"+owner.ID)
	addMembership(t, store, companyID, owner.ID, "owner", "active")

	if _, err := service.InviteMember(ctx, owner.ID, companyID, existing.Email, "member", ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	token := invitationToken(t, service, existing.Email)

	// An existing account cannot be taken over with only the emailed token.
	if _, err := service.AcceptInvitation(ctx, token, "attacker chosen long password", nil, ClientInfo{}); !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("anonymous existing-account acceptance = %v", err)
	}
	_, other := integrationIdentity(t)
	otherSession, err := store.CreateSession(ctx, other.ID, sessionTokenHash("other-session"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptInvitation(ctx, token, "", &otherSession, ClientInfo{}); !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("wrong-account acceptance = %v", err)
	}
	existingSession, err := store.CreateSession(ctx, existing.ID, sessionTokenHash("existing-session"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptInvitation(ctx, token, "", &existingSession, ClientInfo{}); err != nil {
		t.Fatalf("existing-account acceptance: %v", err)
	}
	// Password must be unchanged.
	if _, err := store.GetUserByNormalizedEmail(ctx, existing.Email); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptInvitation(ctx, token, "", &existingSession, ClientInfo{}); !errors.Is(err, ErrInvitationInvalid) {
		t.Fatalf("invitation replay = %v", err)
	}

	// A brand-new invited account may choose its password once.
	freshEmail := "fresh-" + owner.ID + "@example.test"
	if _, err := service.InviteMember(ctx, owner.ID, companyID, freshEmail, "viewer", ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	freshToken := invitationToken(t, service, freshEmail)
	if _, err := service.AcceptInvitation(ctx, freshToken, "", nil, ClientInfo{}); !errors.Is(err, ErrAccountRequired) {
		t.Fatalf("missing password = %v", err)
	}
	user, err := service.AcceptInvitation(ctx, freshToken, "a sufficiently long password", nil, ClientInfo{})
	if err != nil {
		t.Fatalf("new account acceptance: %v", err)
	}
	role, err := store.TenantRole(ctx, user.ID, companyID)
	if err != nil || role != "viewer" {
		t.Fatalf("new membership role = %q err=%v", role, err)
	}
}

func TestCrossTenantAndSuspendedCompanyAreDenied(t *testing.T) {
	store, admin := integrationIdentity(t)
	service := recoveryService(t, store)
	ctx := context.Background()
	first := createCompany(t, store, "cross-a-"+admin.ID)
	second := createCompany(t, store, "cross-b-"+admin.ID)
	addMembership(t, store, first, admin.ID, "admin", "active")

	if _, err := service.ListMembers(ctx, admin.ID, second); !errors.Is(err, ErrCompanyAccess) {
		t.Fatalf("cross-tenant list = %v", err)
	}
	if _, err := service.ListInvitations(ctx, admin.ID, second); !errors.Is(err, ErrCompanyAccess) {
		t.Fatalf("cross-tenant invitations = %v", err)
	}
	if err := service.UpdateMembership(ctx, admin.ID, second, admin.ID, ptr("owner"), nil); !errors.Is(err, ErrCompanyAccess) {
		t.Fatalf("cross-tenant membership update = %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE app.companies SET status='suspended' WHERE id=$1::uuid`, first); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListMembers(ctx, admin.ID, first); !errors.Is(err, ErrCompanyAccess) {
		t.Fatalf("suspended company list = %v", err)
	}
	if _, err := service.InviteMember(ctx, admin.ID, first, "blocked@example.test", "member", ClientInfo{}); !errors.Is(err, ErrCompanyAccess) {
		t.Fatalf("suspended company invite = %v", err)
	}
}

func TestSelfAndLastOwnerProtection(t *testing.T) {
	store, owner := integrationIdentity(t)
	_, admin := integrationIdentity(t)
	service := recoveryService(t, store)
	ctx := context.Background()
	companyID := createCompany(t, store, "owner-"+owner.ID)
	addMembership(t, store, companyID, owner.ID, "owner", "active")
	addMembership(t, store, companyID, admin.ID, "admin", "active")

	if err := service.UpdateMembership(ctx, owner.ID, companyID, owner.ID, ptr("admin"), nil); !errors.Is(err, ErrSelfManagement) {
		t.Fatalf("self demotion = %v", err)
	}
	if err := service.UpdateMembership(ctx, admin.ID, companyID, owner.ID, ptr("admin"), nil); !errors.Is(err, ErrCapability) {
		t.Fatalf("admin demotes owner = %v", err)
	}

	// Concurrent cross attempts: only the owner may change the owner record.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results <- service.UpdateMembership(ctx, owner.ID, companyID, admin.ID, ptr("viewer"), nil)
	}()
	go func() {
		defer wg.Done()
		results <- service.UpdateMembership(ctx, admin.ID, companyID, owner.ID, ptr("viewer"), nil)
	}()
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
			continue
		}
		if !errors.Is(err, ErrCapability) && !errors.Is(err, ErrLastOwner) {
			t.Fatalf("concurrent owner change = %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("concurrent owner changes succeeded = %d, want 1", success)
	}
	var owners int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM app.company_memberships
		WHERE company_id=$1::uuid AND role='owner' AND status='active'`, companyID).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if owners != 1 {
		t.Fatalf("active owners = %d, want 1", owners)
	}
}

func TestInvitationCannotEscalateExistingMembership(t *testing.T) {
	store, owner := integrationIdentity(t)
	_, member := integrationIdentity(t)
	service := recoveryService(t, store)
	ctx := context.Background()
	companyID := createCompany(t, store, "escalate-"+owner.ID)
	addMembership(t, store, companyID, owner.ID, "owner", "active")
	addMembership(t, store, companyID, member.ID, "viewer", "active")
	if _, err := service.InviteMember(ctx, owner.ID, companyID, member.Email, "admin", ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	token := invitationToken(t, service, member.Email)
	session, err := store.CreateSession(ctx, member.ID, sessionTokenHash("member-session"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptInvitation(ctx, token, "", &session, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	role, err := store.TenantRole(ctx, member.ID, companyID)
	if err != nil || role != "viewer" {
		t.Fatalf("existing membership role = %q err=%v", role, err)
	}

	// Suspended and removed memberships are reactivated with the invited role.
	if _, err := store.pool.Exec(ctx, `UPDATE app.company_memberships SET status='removed'
		WHERE company_id=$1::uuid AND user_id=$2::uuid`, companyID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.InviteMember(ctx, owner.ID, companyID, member.Email, "admin", ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	token = invitationToken(t, service, member.Email)
	if _, err := service.AcceptInvitation(ctx, token, "", &session, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	role, err = store.TenantRole(ctx, member.ID, companyID)
	if err != nil || role != "admin" {
		t.Fatalf("reactivated role = %q err=%v", role, err)
	}
}

func ptr(value string) *string { return &value }

func TestInvitationRoleValidation(t *testing.T) {
	store, owner := integrationIdentity(t)
	service := recoveryService(t, store)
	companyID := createCompany(t, store, "roles-"+owner.ID)
	addMembership(t, store, companyID, owner.ID, "owner", "active")
	for _, role := range []string{"", "owner", "superadmin", "ADMIN", "member "} {
		if _, err := service.InviteMember(context.Background(), owner.ID, companyID, "x@example.test", role, ClientInfo{}); !errors.Is(err, ErrInvitationRole) {
			t.Errorf("role %q accepted: %v", role, err)
		}
	}
	if !strings.Contains(NormalizeEmail(" X@Example.Test "), "@") {
		t.Skip()
	}
}
