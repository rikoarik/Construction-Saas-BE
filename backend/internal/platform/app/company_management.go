package app

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrCompanyNotFound    = errors.New("company not found")
	ErrCompanyConflict    = errors.New("company conflict")
	ErrCompanyStatus      = errors.New("company status conflict")
	ErrInvitationConflict = errors.New("invitation conflict")
	ErrInvitationNotFound = errors.New("invitation not found")
	ErrMemberNotFound     = errors.New("administrator not found")
	ErrLastAdmin          = errors.New("last active administrator")
	ErrIdempotency        = errors.New("idempotency key conflict")
)
var companyCode = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,39}$`)

type CompanyProfile struct {
	ID               string     `json:"id"`
	Code             string     `json:"code"`
	Name             string     `json:"name"`
	LegalName        *string    `json:"legal_name"`
	ContactEmail     *string    `json:"contact_email"`
	Phone            *string    `json:"phone"`
	Address          *string    `json:"address"`
	Timezone         string     `json:"timezone"`
	PlanID           *string    `json:"plan_id"`
	SubscriptionPlan *string    `json:"subscription_plan"`
	Status           string     `json:"status"`
	AccessVersion    int64      `json:"access_version"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	LastActivityAt   *time.Time `json:"last_activity_at"`
	SuspendedReason  *string    `json:"suspended_reason"`
	SuspendedBy      *string    `json:"suspended_by"`
	SuspendedAt      *time.Time `json:"suspended_at"`
	MemberCount      int64      `json:"member_count"`
	ProjectCount     int64      `json:"project_count"`
}
type CompanyFilter struct {
	Search, Status, PlanID, Sort string
	Limit, Offset                int
}
type CompanyPage struct {
	Items   []CompanyProfile `json:"items"`
	Limit   int              `json:"limit"`
	Offset  int              `json:"offset"`
	HasMore bool             `json:"has_more"`
	Total   int64            `json:"total"`
}
type CompanyInput struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	LegalName    *string `json:"legal_name"`
	ContactEmail *string `json:"contact_email"`
	Phone        *string `json:"phone"`
	Address      *string `json:"address"`
	Timezone     string  `json:"timezone"`
	PlanID       *string `json:"plan_id"`
	AdminEmail   string  `json:"admin_email"`
}
type CompanyPatch struct {
	Name         *string `json:"name"`
	LegalName    *string `json:"legal_name"`
	ContactEmail *string `json:"contact_email"`
	Phone        *string `json:"phone"`
	Address      *string `json:"address"`
	Timezone     *string `json:"timezone"`
	PlanID       *string `json:"plan_id"`
}
type AdminMember struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type AdminInvitation struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}
type AdminList struct {
	Members     []AdminMember     `json:"members"`
	Invitations []AdminInvitation `json:"invitations"`
}

type CompanyRepository interface {
	List(context.Context, CompanyFilter) (CompanyPage, error)
	Get(context.Context, string) (CompanyProfile, error)
	Create(context.Context, string, string, CompanyInput) (CompanyProfile, error)
	Update(context.Context, string, string, CompanyPatch) (CompanyProfile, error)
	ChangeStatus(context.Context, string, string, string, string) (CompanyProfile, error)
	Admins(context.Context, string) (AdminList, error)
	Invite(context.Context, string, string, string) (AdminInvitation, error)
	Resend(context.Context, string, string, string) (AdminInvitation, error)
	Cancel(context.Context, string, string, string) error
	SetAdminStatus(context.Context, string, string, string, string) (AdminMember, error)
}
type CompanyManagement struct{ Repository CompanyRepository }

func (s CompanyManagement) List(ctx context.Context, f CompanyFilter) (CompanyPage, error) {
	if f.Limit < 1 || f.Limit > 100 || f.Offset < 0 || f.Offset > 10000 || len(f.Search) > 200 || (f.Status != "" && f.Status != "active" && f.Status != "suspended" && f.Status != "provisioning" && f.Status != "archived") {
		return CompanyPage{}, ErrInvalid
	}
	switch f.Sort {
	case "", "-created_at", "created_at", "name", "-name", "code", "-code":
	default:
		return CompanyPage{}, ErrInvalid
	}
	return s.Repository.List(ctx, f)
}
func (s CompanyManagement) Get(ctx context.Context, id string) (CompanyProfile, error) {
	return s.Repository.Get(ctx, id)
}
func validEmail(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) <= 254 && len(value) > 3 && strings.Count(value, "@") == 1 && !strings.ContainsAny(value, " \t\n")
}
func validText(v *string, max int) bool { return v == nil || len(*v) <= max }
func normalizedInput(in CompanyInput) CompanyInput {
	in.Name = strings.TrimSpace(in.Name)
	in.AdminEmail = strings.ToLower(strings.TrimSpace(in.AdminEmail))
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	return in
}
func (s CompanyManagement) Create(ctx context.Context, actor, key string, in CompanyInput) (CompanyProfile, error) {
	in = normalizedInput(in)
	if !companyCode.MatchString(in.Code) || len(strings.TrimSpace(in.Name)) < 1 || len(in.Name) > 200 || !validEmail(in.AdminEmail) || !validText(in.LegalName, 200) || !validText(in.Phone, 40) || !validText(in.Address, 1000) || !validText(in.ContactEmail, 254) || in.ContactEmail != nil && !validEmail(*in.ContactEmail) || len(key) > 128 || len(key) < 1 {
		return CompanyProfile{}, ErrInvalid
	}
	if len(in.Timezone) > 100 {
		return CompanyProfile{}, ErrInvalid
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return CompanyProfile{}, ErrInvalid
	}
	return s.Repository.Create(ctx, actor, key, in)
}
func (s CompanyManagement) Update(ctx context.Context, actor, id string, in CompanyPatch) (CompanyProfile, error) {
	if in.Name == nil && in.LegalName == nil && in.ContactEmail == nil && in.Phone == nil && in.Address == nil && in.Timezone == nil && in.PlanID == nil {
		return CompanyProfile{}, ErrInvalid
	}
	if in.Name != nil && (len(strings.TrimSpace(*in.Name)) == 0 || len(*in.Name) > 200) || !validText(in.LegalName, 200) || !validText(in.Phone, 40) || !validText(in.Address, 1000) || in.ContactEmail != nil && !validEmail(*in.ContactEmail) {
		return CompanyProfile{}, ErrInvalid
	}
	if in.Timezone != nil {
		if len(*in.Timezone) > 100 {
			return CompanyProfile{}, ErrInvalid
		}
		if _, err := time.LoadLocation(*in.Timezone); err != nil {
			return CompanyProfile{}, ErrInvalid
		}
	}
	return s.Repository.Update(ctx, actor, id, in)
}
func (s CompanyManagement) ChangeStatus(ctx context.Context, actor, id, status, reason string) (CompanyProfile, error) {
	if status == "suspended" && (len(strings.TrimSpace(reason)) == 0 || len(reason) > 1000) {
		return CompanyProfile{}, ErrInvalid
	}
	return s.Repository.ChangeStatus(ctx, actor, id, status, reason)
}
func (s CompanyManagement) Invite(ctx context.Context, actor, id, email string) (AdminInvitation, error) {
	if !validEmail(email) {
		return AdminInvitation{}, ErrInvalid
	}
	return s.Repository.Invite(ctx, actor, id, email)
}
