package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const invitationTTL = 7 * 24 * time.Hour

var (
	ErrInvitationExists  = errors.New("a pending invitation already exists")
	ErrInvitationInvalid = errors.New("invitation is invalid, expired, or already used")
	ErrInvitationRole    = errors.New("invitation role is not allowed")
	ErrAlreadyMember     = errors.New("account is already an active member")
	ErrAccountRequired   = errors.New("an account must be provisioned before accepting")
	ErrAuthRequired      = errors.New("sign in with the invited account to accept")
	ErrLastOwner         = errors.New("the company must retain an active owner")
	ErrSelfManagement    = errors.New("members cannot change their own role or status")
	ErrCapability        = errors.New("capability is required")
)

type Invitation struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type Member struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TenantRole returns the actor's live role for a company, or ErrCompanyAccess.
// It is the single authorization entry point for company-scoped operations.
func (s *Store) TenantRole(ctx context.Context, userID, companyID string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	return tenantRoleTx(ctx, tx, userID, companyID)
}

func tenantRoleTx(ctx context.Context, tx pgx.Tx, userID, companyID string) (string, error) {
	var role string
	err := tx.QueryRow(ctx, `
		SELECT m.role
		FROM app.company_memberships m
		JOIN app.companies c ON c.id=m.company_id
		JOIN app.users u ON u.id=m.user_id
		WHERE m.user_id=$1::uuid AND m.company_id=$2::uuid
		  AND m.status='active' AND c.status='active' AND u.status='active'`,
		userID, companyID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrCompanyAccess
	}
	if err != nil {
		return "", fmt.Errorf("load tenant role: %w", err)
	}
	return role, nil
}

func roleHasCapability(ctx context.Context, tx pgx.Tx, role, capability string) (bool, error) {
	var allowed bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM app.tenant_role_capabilities
			WHERE role=$1 AND capability_code=$2
		)`, role, capability).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check tenant capability: %w", err)
	}
	return allowed, nil
}

func requireTenantCapability(ctx context.Context, tx pgx.Tx, userID, companyID, capability string) (string, error) {
	role, err := tenantRoleTx(ctx, tx, userID, companyID)
	if err != nil {
		return "", err
	}
	allowed, err := roleHasCapability(ctx, tx, role, capability)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", ErrCapability
	}
	return role, nil
}

func validTenantRole(role string) bool {
	switch role {
	case "owner", "admin", "member", "viewer":
		return true
	default:
		return false
	}
}

func validInvitationRole(role string) bool {
	switch role {
	case "admin", "member", "viewer":
		return true
	default:
		return false
	}
}

// InviteMember creates a single-use invitation, encrypts the delivery secret,
// and enqueues the outbox event in the same transaction.
func (s *AuthService) InviteMember(ctx context.Context, actorID, companyID, email, role string, client ClientInfo) (Invitation, error) {
	email = strings.TrimSpace(email)
	normalized := NormalizeEmail(email)
	if normalized == "" || !strings.Contains(normalized, "@") || !validInvitationRole(role) {
		return Invitation{}, ErrInvitationRole
	}
	if _, err := s.frontendURL(); err != nil {
		return Invitation{}, err
	}
	token, err := randomToken(32)
	if err != nil {
		return Invitation{}, err
	}
	box, err := s.emailBox()
	if err != nil {
		return Invitation{}, err
	}
	ciphertext, nonce, err := box.Encrypt([]byte(token), emailTokenAAD("company_invitation", ""))
	if err != nil {
		return Invitation{}, err
	}
	expires := time.Now().Add(invitationTTL)

	tx, err := s.Store.pool.Begin(ctx)
	if err != nil {
		return Invitation{}, err
	}
	defer tx.Rollback(ctx)
	actorRole, err := requireTenantCapability(ctx, tx, actorID, companyID, "member.manage")
	if err != nil {
		return Invitation{}, err
	}
	var companyActive bool
	if err := tx.QueryRow(ctx, `SELECT status='active' FROM app.companies WHERE id=$1::uuid FOR SHARE`, companyID).Scan(&companyActive); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Invitation{}, ErrCompanyAccess
		}
		return Invitation{}, err
	}
	if !companyActive {
		return Invitation{}, ErrCompanyAccess
	}
	if actorRole != "owner" && role == "admin" {
		// Only an owner may hand out owner-equivalent administration.
		return Invitation{}, ErrCapability
	}
	var existing bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM app.company_invitations
		WHERE company_id=$1::uuid AND email_normalized=$2
		  AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>now()
	)`, companyID, normalized).Scan(&existing); err != nil {
		return Invitation{}, err
	}
	if existing {
		return Invitation{}, ErrInvitationExists
	}
	// Keep the database partial unique index usable after a pending link expires.
	if _, err := tx.Exec(ctx, `UPDATE app.company_invitations SET revoked_at=now(),updated_at=now()
		WHERE company_id=$1::uuid AND email_normalized=$2 AND accepted_at IS NULL
		AND revoked_at IS NULL AND expires_at<=now()`, companyID, normalized); err != nil {
		return Invitation{}, err
	}
	var invitation Invitation
	err = tx.QueryRow(ctx, `
		INSERT INTO app.company_invitations
			(company_id,email,email_normalized,token_hash,role,expires_at,created_by)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,$7::uuid)
		RETURNING id::text,email,role,expires_at,created_at`,
		companyID, email, normalized, tokenDigest(token), role, expires, actorID).Scan(
		&invitation.ID, &invitation.Email, &invitation.Role, &invitation.ExpiresAt, &invitation.CreatedAt)
	if err != nil {
		return Invitation{}, fmt.Errorf("create invitation: %w", err)
	}
	var deliveryID string
	err = tx.QueryRow(ctx, `INSERT INTO app.auth_email_deliveries
		(user_id, recipient_email, template, token_ciphertext, token_nonce, link_expires_at)
		VALUES (NULL,$1,'company_invitation',$2,$3,$4) RETURNING id::text`,
		email, ciphertext, nonce, expires).Scan(&deliveryID)
	if err != nil {
		return Invitation{}, fmt.Errorf("create invitation delivery: %w", err)
	}
	payload, err := json.Marshal(map[string]string{
		"delivery_id": deliveryID, "company_id": companyID,
	})
	if err != nil {
		return Invitation{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.outbox_events (company_id,event_type,payload,idempotency_key)
		VALUES ($1::uuid,'email.delivery',$2::jsonb,$3)`,
		companyID, payload, "email.delivery:"+deliveryID); err != nil {
		return Invitation{}, fmt.Errorf("enqueue invitation email: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, err
	}
	return invitation, nil
}

func (s *AuthService) ListInvitations(ctx context.Context, actorID, companyID string) ([]Invitation, error) {
	tx, err := s.Store.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := requireTenantCapability(ctx, tx, actorID, companyID, "member.read"); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text,email,role,expires_at,created_at
		FROM app.company_invitations
		WHERE company_id=$1::uuid AND accepted_at IS NULL AND revoked_at IS NULL
		ORDER BY created_at DESC, id DESC`, companyID)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	defer rows.Close()
	result := make([]Invitation, 0)
	for rows.Next() {
		var invitation Invitation
		if err := rows.Scan(&invitation.ID, &invitation.Email, &invitation.Role,
			&invitation.ExpiresAt, &invitation.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, invitation)
	}
	return result, rows.Err()
}

func (s *AuthService) RevokeInvitation(ctx context.Context, actorID, companyID, invitationID string) error {
	tx, err := s.Store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := requireTenantCapability(ctx, tx, actorID, companyID, "member.manage"); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE app.company_invitations
		SET revoked_at=now(), updated_at=now()
		WHERE id=$1::uuid AND company_id=$2::uuid
		  AND revoked_at IS NULL AND accepted_at IS NULL AND expires_at>now()`,
		invitationID, companyID)
	if err != nil {
		return fmt.Errorf("revoke invitation: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrInvitationInvalid
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.security_events (user_id,event_type,metadata)
		VALUES ($1::uuid,'tenant.invitation_revoked',jsonb_build_object('company_id',$2::text,'invitation_id',$3::text))`,
		actorID, companyID, invitationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AcceptInvitation consumes a single-use invitation. An existing account must
// present a session for that exact account; the password is never changed.
// Only a brand-new account may set a password, and only through this
// token-possession flow.
func (s *AuthService) AcceptInvitation(ctx context.Context, token, password string, session *Session, client ClientInfo) (User, error) {
	if len(token) < 20 || len(token) > 200 {
		return User{}, ErrInvitationInvalid
	}
	if password != "" && !ValidNewPassword(password) {
		return User{}, ErrPasswordPolicy
	}
	tx, err := s.Store.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)

	var (
		invitationID string
		companyID    string
		email        string
		normalized   string
		role         string
		expiresAt    time.Time
	)
	// Lock company before invitation, matching platform administrative mutations.
	if err := tx.QueryRow(ctx, `SELECT company_id::text FROM app.company_invitations WHERE token_hash=$1`, tokenDigest(token)).Scan(&companyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrInvitationInvalid
		}
		return User{}, err
	}
	var companyStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM app.companies WHERE id=$1::uuid FOR UPDATE`, companyID).Scan(&companyStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrInvitationInvalid
		}
		return User{}, err
	}
	if companyStatus != "active" && companyStatus != "provisioning" {
		return User{}, ErrInvitationInvalid
	}
	err = tx.QueryRow(ctx, `
		SELECT id::text,company_id::text,email,email_normalized,role,expires_at
		FROM app.company_invitations
		WHERE token_hash=$1 AND accepted_at IS NULL AND revoked_at IS NULL
		FOR UPDATE`, tokenDigest(token)).Scan(
		&invitationID, &companyID, &email, &normalized, &role, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrInvitationInvalid
	}
	if err != nil {
		return User{}, fmt.Errorf("load invitation: %w", err)
	}
	if !expiresAt.After(time.Now()) {
		return User{}, ErrInvitationInvalid
	}
	if companyStatus == "provisioning" && role != "admin" {
		return User{}, ErrInvitationInvalid
	}
	// A newly provisioned workspace becomes active only when its first admin
	// accepts; no tenant content is reachable while it is provisioning.

	var user User
	err = tx.QueryRow(ctx, `
		SELECT u.id::text,u.email,u.email_normalized,c.password_hash,u.status,
		       u.email_verified_at,u.security_version,u.created_at,u.updated_at
		FROM app.users u JOIN app.user_credentials c ON c.user_id=u.id
		WHERE u.email_normalized=$1 FOR UPDATE OF u`, normalized).Scan(
		&user.ID, &user.Email, &user.EmailNormalized, &user.PasswordHash, &user.Status,
		&user.EmailVerifiedAt, &user.SecurityVersion, &user.CreatedAt, &user.UpdatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if password == "" {
			return User{}, ErrAccountRequired
		}
		hash, hashErr := s.Passwords.Hash(password)
		if hashErr != nil {
			return User{}, hashErr
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO app.users (email,email_normalized,password_hash,status,email_verified_at)
			VALUES ($1,$2,$3,'active',now())
			RETURNING id::text,email,email_normalized,status,email_verified_at,
			          security_version,created_at,updated_at`,
			email, normalized, hash).Scan(&user.ID, &user.Email, &user.EmailNormalized,
			&user.Status, &user.EmailVerifiedAt, &user.SecurityVersion, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return User{}, fmt.Errorf("create invited account: %w", err)
		}
		user.PasswordHash = hash
		if _, err := tx.Exec(ctx, `INSERT INTO app.user_credentials (user_id,password_hash)
			VALUES ($1::uuid,$2) ON CONFLICT (user_id) DO UPDATE SET password_hash=EXCLUDED.password_hash`,
			user.ID, hash); err != nil {
			return User{}, fmt.Errorf("create invited credentials: %w", err)
		}
	case err != nil:
		return User{}, fmt.Errorf("load invited account: %w", err)
	default:
		if session == nil || session.UserID != user.ID {
			return User{}, ErrAuthRequired
		}
		if user.Status != "active" {
			return User{}, ErrInvitationInvalid
		}
	}

	var membershipStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM app.company_memberships
		WHERE company_id=$1::uuid AND user_id=$2::uuid FOR UPDATE`, companyID, user.ID).Scan(&membershipStatus)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if _, err := tx.Exec(ctx, `INSERT INTO app.company_memberships
			(company_id,user_id,role,status) VALUES ($1::uuid,$2::uuid,$3,'active')`,
			companyID, user.ID, role); err != nil {
			return User{}, fmt.Errorf("create membership: %w", err)
		}
	case err != nil:
		return User{}, err
	case membershipStatus == "active":
		// Already a member: accept the invitation without changing the
		// existing role, so an old invitation cannot escalate privileges.
	default:
		if _, err := tx.Exec(ctx, `UPDATE app.company_memberships
			SET role=$3,status='active',updated_at=now()
			WHERE company_id=$1::uuid AND user_id=$2::uuid`,
			companyID, user.ID, role); err != nil {
			return User{}, fmt.Errorf("reactivate membership: %w", err)
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE app.company_invitations
		SET accepted_at=now(), accepted_by=$2::uuid, updated_at=now()
		WHERE id=$1::uuid AND accepted_at IS NULL AND revoked_at IS NULL`, invitationID, user.ID)
	if err != nil || tag.RowsAffected() != 1 {
		return User{}, ErrInvitationInvalid
	}
	if companyStatus == "provisioning" {
		if _, err := tx.Exec(ctx, `UPDATE app.companies SET status='active',access_version=access_version+1,updated_at=now() WHERE id=$1::uuid`, companyID); err != nil {
			return User{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app.audit_logs(company_id,actor_id,action,entity_type,entity_id,metadata_json)
			VALUES($1::uuid,$2::uuid,'company.activated','company',$1::uuid,'{}'::jsonb)`, companyID, user.ID); err != nil {
			return User{}, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.audit_logs(company_id,actor_id,action,entity_type,entity_id,metadata_json)
		VALUES($1::uuid,$2::uuid,'company.invitation_accepted','company_invitation',$3::uuid,'{}'::jsonb)`, companyID, user.ID, invitationID); err != nil {
		return User{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.security_events (user_id,event_type,metadata)
		VALUES ($1::uuid,'tenant.invitation_accepted',jsonb_build_object('company_id',$2::text))`,
		user.ID, companyID); err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *AuthService) ListMembers(ctx context.Context, actorID, companyID string) ([]Member, error) {
	tx, err := s.Store.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := requireTenantCapability(ctx, tx, actorID, companyID, "member.read"); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT u.id::text,u.email,m.role,m.status,m.created_at,m.updated_at
		FROM app.company_memberships m JOIN app.users u ON u.id=m.user_id
		WHERE m.company_id=$1::uuid
		ORDER BY m.role,u.email`, companyID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()
	result := make([]Member, 0)
	for rows.Next() {
		var member Member
		if err := rows.Scan(&member.UserID, &member.Email, &member.Role,
			&member.Status, &member.CreatedAt, &member.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, member)
	}
	return result, rows.Err()
}

func (s *AuthService) UpdateMembership(ctx context.Context, actorID, companyID, targetID string, role, status *string) error {
	if role == nil && status == nil {
		return ErrCapability
	}
	if role != nil && !validTenantRole(*role) {
		return ErrCapability
	}
	if status != nil && *status != "active" && *status != "suspended" && *status != "removed" {
		return ErrCapability
	}
	tx, err := s.Store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	actorRole, err := requireTenantCapability(ctx, tx, actorID, companyID, "member.manage")
	if err != nil {
		return err
	}
	if targetID == actorID {
		return ErrSelfManagement
	}
	// Lock the company row first so concurrent last-owner removals serialize.
	var companyIDLock string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM app.companies WHERE id=$1::uuid FOR UPDATE`, companyID).Scan(&companyIDLock); err != nil {
		return ErrCompanyAccess
	}
	var currentRole, currentStatus string
	err = tx.QueryRow(ctx, `SELECT role,status FROM app.company_memberships
		WHERE company_id=$1::uuid AND user_id=$2::uuid FOR UPDATE`, companyID, targetID).Scan(&currentRole, &currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if actorRole != "owner" && (currentRole == "owner" || (role != nil && *role == "owner")) {
		return ErrCapability
	}
	nextRole, nextStatus := currentRole, currentStatus
	if role != nil {
		nextRole = *role
	}
	if status != nil {
		nextStatus = *status
	}
	if currentRole == "owner" && currentStatus == "active" && (nextRole != "owner" || nextStatus != "active") {
		var owners int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM app.company_memberships
			WHERE company_id=$1::uuid AND role='owner' AND status='active'`, companyID).Scan(&owners); err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE app.company_memberships
		SET role=$3,status=$4,updated_at=now()
		WHERE company_id=$1::uuid AND user_id=$2::uuid`,
		companyID, targetID, nextRole, nextStatus); err != nil {
		return fmt.Errorf("update membership: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.security_events (user_id,event_type,metadata)
		VALUES ($1::uuid,'tenant.membership_updated',
		jsonb_build_object('company_id',$2::text,'target_user_id',$3::text,'role',$4::text,'status',$5::text))`,
		actorID, companyID, targetID, nextRole, nextStatus); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
