package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"construction-saas/backend/internal/httpapi"
	"construction-saas/backend/internal/identity"
	"construction-saas/backend/internal/platform/app"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Companies struct {
	Pool *pgxpool.Pool
	Auth *identity.AuthService
}

const companyColumns = `c.id::text,c.code,c.name,c.legal_name,c.contact_email,c.phone,c.address,c.timezone,c.plan_id::text,
 (SELECT p.name FROM app.plans p WHERE p.id=c.plan_id),c.status,c.access_version,c.created_at,c.updated_at,
 (SELECT max(a.occurred_at) FROM app.audit_logs a WHERE a.company_id=c.id),
 c.suspended_reason,c.suspended_by::text,c.suspended_at,
 (SELECT count(*) FROM app.company_memberships m WHERE m.company_id=c.id AND m.status='active'),
 (SELECT count(*) FROM app.projects p WHERE p.company_id=c.id)`

func scanCompany(row pgx.Row) (app.CompanyProfile, error) {
	var c app.CompanyProfile
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.LegalName, &c.ContactEmail, &c.Phone, &c.Address, &c.Timezone, &c.PlanID, &c.SubscriptionPlan, &c.Status, &c.AccessVersion, &c.CreatedAt, &c.UpdatedAt, &c.LastActivityAt, &c.SuspendedReason, &c.SuspendedBy, &c.SuspendedAt, &c.MemberCount, &c.ProjectCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, app.ErrCompanyNotFound
	}
	return c, err
}
func (r Companies) Get(ctx context.Context, id string) (app.CompanyProfile, error) {
	return scanCompany(r.Pool.QueryRow(ctx, `SELECT `+companyColumns+` FROM app.companies c WHERE c.id=$1::uuid`, id))
}
func (r Companies) List(ctx context.Context, f app.CompanyFilter) (app.CompanyPage, error) {
	p := app.CompanyPage{Items: []app.CompanyProfile{}, Limit: f.Limit, Offset: f.Offset}
	order := map[string]string{"": "c.created_at DESC,c.id DESC", "-created_at": "c.created_at DESC,c.id DESC", "created_at": "c.created_at,c.id", "name": "c.name,c.id", "-name": "c.name DESC,c.id DESC", "code": "c.code,c.id", "-code": "c.code DESC,c.id DESC"}[f.Sort]
	where := ` FROM app.companies c WHERE ($1='' OR c.name ILIKE '%'||$1||'%' OR c.code ILIKE '%'||$1||'%' OR c.contact_email ILIKE '%'||$1||'%') AND ($2='' OR c.status=$2) AND ($3='' OR c.plan_id::text=$3)`
	search := strings.TrimSpace(f.Search)
	// Search is bound as a parameter; SQL sort expressions are allowlisted.
	args := []any{search, f.Status, f.PlanID}
	if err := r.Pool.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT `+companyColumns+where+` ORDER BY `+order+` LIMIT $4 OFFSET $5`, search, f.Status, f.PlanID, f.Limit+1, f.Offset)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanCompany(rows)
		if err != nil {
			return p, err
		}
		if len(p.Items) == f.Limit {
			p.HasMore = true
			break
		}
		p.Items = append(p.Items, c)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	return p, nil
}
func dbConflict(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505":
			return app.ErrCompanyConflict
		case "23503", "23514", "22P02", "22001":
			return app.ErrInvalid
		}
	}
	return err
}
func audit(ctx context.Context, tx pgx.Tx, actor, id, action, entity, entityID string, meta any) error {
	payload, _ := json.Marshal(meta)
	_, err := tx.Exec(ctx, `INSERT INTO app.audit_logs(company_id,actor_id,action,entity_type,entity_id,request_id,metadata_json)
 VALUES($1::uuid,$2::uuid,$3,$4,$5::uuid,$6,$7::jsonb)`, id, actor, action, entity, entityID, httpapi.RequestID(ctx), payload)
	return err
}
func lockCompany(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM app.companies WHERE id=$1::uuid FOR UPDATE`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", app.ErrCompanyNotFound
	}
	return status, err
}
func (r Companies) Create(ctx context.Context, actor, key string, in app.CompanyInput) (app.CompanyProfile, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return app.CompanyProfile{}, err
	}
	defer tx.Rollback(ctx)
	raw, _ := json.Marshal(in)
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	var savedHash string
	var response []byte
	err = tx.QueryRow(ctx, `INSERT INTO app.platform_idempotency(actor_id,operation,key,request_hash) VALUES($1::uuid,'company.create',$2,$3)
 ON CONFLICT(actor_id,operation,key) DO UPDATE SET key=EXCLUDED.key RETURNING request_hash,response_json`, actor, key, hash).Scan(&savedHash, &response)
	if err != nil {
		return app.CompanyProfile{}, dbConflict(err)
	}
	if savedHash != hash {
		return app.CompanyProfile{}, app.ErrIdempotency
	}
	if len(response) > 0 {
		var c app.CompanyProfile
		if err = json.Unmarshal(response, &c); err != nil {
			return c, err
		}
		if err = tx.Commit(ctx); err != nil {
			return c, err
		}
		return c, nil
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO app.companies(code,slug,name,legal_name,contact_email,phone,address,timezone,plan_id,status)
 VALUES($1,$1,$2,$3,$4,$5,$6,$7,$8::uuid,'provisioning') RETURNING id::text`, in.Code, strings.TrimSpace(in.Name), in.LegalName, in.ContactEmail, in.Phone, in.Address, in.Timezone, in.PlanID).Scan(&id)
	if err != nil {
		return app.CompanyProfile{}, dbConflict(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app.company_settings(company_id,timezone) VALUES($1::uuid,$2)`, id, in.Timezone); err != nil {
		return app.CompanyProfile{}, err
	}
	invite, err := r.Auth.QueuePlatformInvitation(ctx, tx, actor, id, in.AdminEmail)
	if err != nil {
		return app.CompanyProfile{}, dbConflict(err)
	}
	if err = audit(ctx, tx, actor, id, "company.registered", "company", id, map[string]any{"code": in.Code, "invitation_id": invite.ID}); err != nil {
		return app.CompanyProfile{}, err
	}
	c, err := scanCompany(tx.QueryRow(ctx, `SELECT `+companyColumns+` FROM app.companies c WHERE c.id=$1::uuid`, id))
	if err != nil {
		return c, err
	}
	payload, _ := json.Marshal(c)
	if _, err = tx.Exec(ctx, `UPDATE app.platform_idempotency SET response_json=$4::jsonb WHERE actor_id=$1::uuid AND operation='company.create' AND key=$2 AND request_hash=$3`, actor, key, hash, payload); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func changedCompanyFields(in app.CompanyPatch) []string {
	fields := []string{}
	if in.Name != nil {
		fields = append(fields, "name")
	}
	if in.LegalName != nil {
		fields = append(fields, "legal_name")
	}
	if in.ContactEmail != nil {
		fields = append(fields, "contact_email")
	}
	if in.Phone != nil {
		fields = append(fields, "phone")
	}
	if in.Address != nil {
		fields = append(fields, "address")
	}
	if in.Timezone != nil {
		fields = append(fields, "timezone")
	}
	if in.PlanID != nil {
		fields = append(fields, "plan_id")
	}
	return fields
}
func (r Companies) Update(ctx context.Context, actor, id string, in app.CompanyPatch) (app.CompanyProfile, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return app.CompanyProfile{}, err
	}
	defer tx.Rollback(ctx)
	status, err := lockCompany(ctx, tx, id)
	if err != nil {
		return app.CompanyProfile{}, err
	}
	if status == "archived" {
		return app.CompanyProfile{}, app.ErrCompanyStatus
	}
	var old app.CompanyProfile
	old, err = scanCompany(tx.QueryRow(ctx, `SELECT `+companyColumns+` FROM app.companies c WHERE c.id=$1::uuid`, id))
	if err != nil {
		return old, err
	}
	_, err = tx.Exec(ctx, `UPDATE app.companies SET name=coalesce($2,name),legal_name=coalesce($3,legal_name),contact_email=coalesce($4,contact_email),phone=coalesce($5,phone),address=coalesce($6,address),timezone=coalesce($7,timezone),plan_id=coalesce($8::uuid,plan_id),updated_at=now() WHERE id=$1::uuid`, id, in.Name, in.LegalName, in.ContactEmail, in.Phone, in.Address, in.Timezone, in.PlanID)
	if err != nil {
		return app.CompanyProfile{}, dbConflict(err)
	}
	if in.Timezone != nil {
		if _, err = tx.Exec(ctx, `UPDATE app.company_settings SET timezone=$2,updated_at=now() WHERE company_id=$1::uuid`, id, *in.Timezone); err != nil {
			return app.CompanyProfile{}, err
		}
	}
	if err = audit(ctx, tx, actor, id, "company.updated", "company", id, map[string]any{"changed_fields": changedCompanyFields(in), "previous_status": old.Status}); err != nil {
		return app.CompanyProfile{}, err
	}
	c, err := scanCompany(tx.QueryRow(ctx, `SELECT `+companyColumns+` FROM app.companies c WHERE c.id=$1::uuid`, id))
	if err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (r Companies) ChangeStatus(ctx context.Context, actor, id, next, reason string) (app.CompanyProfile, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return app.CompanyProfile{}, err
	}
	defer tx.Rollback(ctx)
	previous, err := lockCompany(ctx, tx, id)
	if err != nil {
		return app.CompanyProfile{}, err
	}
	if next == "suspended" && previous != "active" || next == "active" && previous != "suspended" {
		return app.CompanyProfile{}, app.ErrCompanyStatus
	}
	if next != "suspended" && next != "active" {
		return app.CompanyProfile{}, app.ErrInvalid
	}
	if next == "suspended" {
		_, err = tx.Exec(ctx, `UPDATE app.companies SET status='suspended',suspended_reason=$2,suspended_by=$3::uuid,suspended_at=now(),access_version=access_version+1,updated_at=now() WHERE id=$1::uuid`, id, strings.TrimSpace(reason), actor)
	} else {
		_, err = tx.Exec(ctx, `UPDATE app.companies SET status='active',suspended_reason=NULL,suspended_by=NULL,suspended_at=NULL,access_version=access_version+1,updated_at=now() WHERE id=$1::uuid`, id)
	}
	if err != nil {
		return app.CompanyProfile{}, err
	}
	action := "company.reactivated"
	if next == "suspended" {
		action = "company.suspended"
	}
	if err = audit(ctx, tx, actor, id, action, "company", id, map[string]any{"from": previous, "to": next, "reason": reason}); err != nil {
		return app.CompanyProfile{}, err
	}
	c, err := scanCompany(tx.QueryRow(ctx, `SELECT `+companyColumns+` FROM app.companies c WHERE c.id=$1::uuid`, id))
	if err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (r Companies) Admins(ctx context.Context, id string) (app.AdminList, error) {
	result := app.AdminList{Members: []app.AdminMember{}, Invitations: []app.AdminInvitation{}}
	if _, err := r.Get(ctx, id); err != nil {
		return result, err
	}
	rows, err := r.Pool.Query(ctx, `SELECT m.user_id::text,u.email,m.role,m.status,m.created_at,m.updated_at FROM app.company_memberships m JOIN app.users u ON u.id=m.user_id WHERE m.company_id=$1::uuid AND m.role IN ('owner','admin') ORDER BY u.email,m.user_id`, id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var m app.AdminMember
		if err = rows.Scan(&m.UserID, &m.Email, &m.Role, &m.Status, &m.CreatedAt, &m.UpdatedAt); err != nil {
			rows.Close()
			return result, err
		}
		result.Members = append(result.Members, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	inv, err := r.Pool.Query(ctx, `SELECT id::text,email,CASE WHEN revoked_at IS NOT NULL THEN 'revoked' WHEN accepted_at IS NOT NULL THEN 'accepted' WHEN expires_at<=now() THEN 'expired' ELSE 'pending' END,expires_at,created_at FROM app.company_invitations WHERE company_id=$1::uuid AND role='admin' ORDER BY created_at DESC,id DESC`, id)
	if err != nil {
		return result, err
	}
	defer inv.Close()
	for inv.Next() {
		var i app.AdminInvitation
		if err = inv.Scan(&i.ID, &i.Email, &i.Status, &i.ExpiresAt, &i.CreatedAt); err != nil {
			return result, err
		}
		result.Invitations = append(result.Invitations, i)
	}
	return result, inv.Err()
}
func (r Companies) Invite(ctx context.Context, actor, id, email string) (app.AdminInvitation, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return app.AdminInvitation{}, err
	}
	defer tx.Rollback(ctx)
	status, err := lockCompany(ctx, tx, id)
	if err != nil {
		return app.AdminInvitation{}, err
	}
	if status != "active" && status != "provisioning" {
		return app.AdminInvitation{}, app.ErrCompanyStatus
	}
	var existing bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app.company_memberships m JOIN app.users u ON u.id=m.user_id WHERE m.company_id=$1::uuid AND u.email_normalized=$2 AND m.status='active')`, id, identity.NormalizeEmail(email)).Scan(&existing)
	if err != nil {
		return app.AdminInvitation{}, err
	}
	if existing {
		return app.AdminInvitation{}, app.ErrInvitationConflict
	}
	// Expired invitations are explicitly revoked before issuing a fresh token.
	_, err = tx.Exec(ctx, `UPDATE app.company_invitations SET revoked_at=now(),updated_at=now() WHERE company_id=$1::uuid AND email_normalized=$2 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at<=now()`, id, identity.NormalizeEmail(email))
	if err != nil {
		return app.AdminInvitation{}, err
	}
	i, err := r.Auth.QueuePlatformInvitation(ctx, tx, actor, id, email)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return app.AdminInvitation{}, app.ErrInvitationConflict
		}
		return app.AdminInvitation{}, err
	}
	if err = audit(ctx, tx, actor, id, "company.admin_invited", "company_invitation", i.ID, map[string]any{"role": "admin"}); err != nil {
		return app.AdminInvitation{}, err
	}
	return app.AdminInvitation{ID: i.ID, Email: i.Email, Status: "pending", ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt}, tx.Commit(ctx)
}
func (r Companies) Resend(ctx context.Context, actor, id, invitationID string) (app.AdminInvitation, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return app.AdminInvitation{}, err
	}
	defer tx.Rollback(ctx)
	status, err := lockCompany(ctx, tx, id)
	if err != nil {
		return app.AdminInvitation{}, err
	}
	if status != "active" && status != "provisioning" {
		return app.AdminInvitation{}, app.ErrCompanyStatus
	}
	var email string
	var delivery *string
	err = tx.QueryRow(ctx, `SELECT email,delivery_id::text FROM app.company_invitations WHERE id=$1::uuid AND company_id=$2::uuid AND role='admin' AND accepted_at IS NULL AND revoked_at IS NULL FOR UPDATE`, invitationID, id).Scan(&email, &delivery)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.AdminInvitation{}, app.ErrInvitationNotFound
	}
	if err != nil {
		return app.AdminInvitation{}, err
	}
	// Old queued deliveries are invalidated before token rotation.
	if delivery != nil {
		if _, err = tx.Exec(ctx, `UPDATE app.auth_email_deliveries SET token_ciphertext=NULL,token_nonce=NULL,delivered_at=now() WHERE id=$1::uuid AND delivered_at IS NULL`, *delivery); err != nil {
			return app.AdminInvitation{}, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE app.company_invitations SET revoked_at=now(),updated_at=now() WHERE id=$1::uuid`, invitationID); err != nil {
		return app.AdminInvitation{}, err
	}
	i, err := r.Auth.QueuePlatformInvitation(ctx, tx, actor, id, email)
	if err != nil {
		return app.AdminInvitation{}, err
	}
	if err = audit(ctx, tx, actor, id, "company.admin_invitation_resent", "company_invitation", i.ID, map[string]any{"previous_invitation_id": invitationID}); err != nil {
		return app.AdminInvitation{}, err
	}
	return app.AdminInvitation{ID: i.ID, Email: i.Email, Status: "pending", ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt}, tx.Commit(ctx)
}
func (r Companies) Cancel(ctx context.Context, actor, id, invitationID string) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = lockCompany(ctx, tx, id); err != nil {
		return err
	}
	var delivery *string
	err = tx.QueryRow(ctx, `UPDATE app.company_invitations SET revoked_at=now(),updated_at=now() WHERE id=$1::uuid AND company_id=$2::uuid AND role='admin' AND accepted_at IS NULL AND revoked_at IS NULL RETURNING delivery_id::text`, invitationID, id).Scan(&delivery)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ErrInvitationNotFound
	}
	if err != nil {
		return err
	}
	if delivery != nil {
		if _, err = tx.Exec(ctx, `UPDATE app.auth_email_deliveries SET token_ciphertext=NULL,token_nonce=NULL,delivered_at=now() WHERE id=$1::uuid AND delivered_at IS NULL`, *delivery); err != nil {
			return err
		}
	}
	if err = audit(ctx, tx, actor, id, "company.admin_invitation_revoked", "company_invitation", invitationID, map[string]any{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r Companies) SetAdminStatus(ctx context.Context, actor, id, userID, status string) (app.AdminMember, error) {
	if status != "active" && status != "suspended" {
		return app.AdminMember{}, app.ErrInvalid
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return app.AdminMember{}, err
	}
	defer tx.Rollback(ctx)
	companyStatus, err := lockCompany(ctx, tx, id)
	if err != nil {
		return app.AdminMember{}, err
	}
	if companyStatus != "active" {
		return app.AdminMember{}, app.ErrCompanyStatus
	}
	var m app.AdminMember
	err = tx.QueryRow(ctx, `SELECT m.user_id::text,u.email,m.role,m.status,m.created_at,m.updated_at FROM app.company_memberships m JOIN app.users u ON u.id=m.user_id WHERE m.company_id=$1::uuid AND m.user_id=$2::uuid AND m.role IN ('owner','admin') FOR UPDATE OF m`, id, userID).Scan(&m.UserID, &m.Email, &m.Role, &m.Status, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, app.ErrMemberNotFound
	}
	if err != nil {
		return m, err
	}
	if m.Status == "removed" {
		return m, app.ErrMemberNotFound
	}
	if m.Status == status {
		if err = tx.Commit(ctx); err != nil {
			return m, err
		}
		return m, nil
	}
	if m.Role == "owner" && m.Status == "active" && status == "suspended" {
		return m, app.ErrLastAdmin
	}
	if m.Status == "active" && status == "suspended" {
		var count int
		err = tx.QueryRow(ctx, `SELECT count(*) FROM app.company_memberships WHERE company_id=$1::uuid AND role IN ('owner','admin') AND status='active'`, id).Scan(&count)
		if err != nil {
			return m, err
		}
		if count <= 1 {
			return m, app.ErrLastAdmin
		}
	}
	err = tx.QueryRow(ctx, `UPDATE app.company_memberships SET status=$3,updated_at=now() WHERE company_id=$1::uuid AND user_id=$2::uuid RETURNING updated_at`, id, userID, status).Scan(&m.UpdatedAt)
	if err != nil {
		return m, err
	}
	m.Status = status
	if err = audit(ctx, tx, actor, id, "company.admin_status_changed", "company_membership", userID, map[string]any{"status": status}); err != nil {
		return m, err
	}
	return m, tx.Commit(ctx)
}

var _ app.CompanyRepository = Companies{}
