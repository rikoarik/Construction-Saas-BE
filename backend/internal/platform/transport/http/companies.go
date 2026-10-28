package platformhttp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"construction-saas/backend/internal/httpapi"
	"construction-saas/backend/internal/identity"
	"construction-saas/backend/internal/platform/app"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (h Handler) registerCompanies(mux *http.ServeMux) {
	if h.Companies.Repository == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/platform/companies", h.companyRoute("platform.company.read", h.listCompanies))
	mux.HandleFunc("POST /api/v1/platform/companies", h.companyRoute("platform.company.create", h.createCompany))
	mux.HandleFunc("GET /api/v1/platform/companies/{id}", h.companyRoute("platform.company.read", h.getCompany))
	mux.HandleFunc("GET /api/v1/platform/companies/{id}/usage", h.companyRoute("platform.company.read", h.getCompanyUsage))
	mux.HandleFunc("PATCH /api/v1/platform/companies/{id}", h.companyRoute("platform.company.update", h.updateCompany))
	mux.HandleFunc("POST /api/v1/platform/companies/{id}/suspend", h.companyRoute("platform.company.suspend", h.suspendCompany))
	mux.HandleFunc("POST /api/v1/platform/companies/{id}/reactivate", h.companyRoute("platform.company.reactivate", h.reactivateCompany))
	mux.HandleFunc("GET /api/v1/platform/companies/{id}/members", h.companyRoute("platform.member.read", h.companyAdmins))
	mux.HandleFunc("POST /api/v1/platform/companies/{id}/invitations", h.companyRoute("platform.member.invite", h.inviteAdmin))
	mux.HandleFunc("POST /api/v1/platform/companies/{id}/invitations/{invitationId}/resend", h.companyRoute("platform.member.invite", h.resendAdmin))
	mux.HandleFunc("DELETE /api/v1/platform/companies/{id}/invitations/{invitationId}", h.companyRoute("platform.member.revoke", h.cancelAdmin))
	mux.HandleFunc("PATCH /api/v1/platform/companies/{id}/members/{userId}", h.companyRoute("platform.member.revoke", h.adminStatus))
}
func (h Handler) companyRoute(capability string, next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		cookie, err := r.Cookie(identity.SessionCookieName)
		if err != nil {
			httpapi.WriteError(w, r, httpapi.ErrUnauthenticated)
			return
		}
		session, err := h.Auth.CurrentUser(r.Context(), cookie)
		if err != nil {
			httpapi.WriteError(w, r, httpapi.ErrUnauthenticated)
			return
		}
		if err = h.Auth.PlatformSession(r.Context(), session); err != nil {
			code := "PLATFORM_PERMISSION_DENIED"
			if errors.Is(err, identity.ErrMFARequired) {
				code = "MFA_REQUIRED"
			}
			httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: code})
			return
		}
		caps, err := h.Auth.Store.PlatformCapabilities(r.Context(), session.UserID)
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		for _, c := range caps {
			if c == capability {
				next(w, r, session.UserID)
				return
			}
		}
		httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: "PLATFORM_PERMISSION_DENIED"})
	}
}
func companyError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	status, code := 500, "INTERNAL_ERROR"
	switch {
	case errors.Is(err, app.ErrInvalid):
		status, code = 400, "VALIDATION_ERROR"
	case errors.Is(err, app.ErrCompanyNotFound):
		status, code = 404, "COMPANY_NOT_FOUND"
	case errors.Is(err, app.ErrMemberNotFound):
		status, code = 404, "MEMBER_NOT_FOUND"
	case errors.Is(err, app.ErrInvitationNotFound):
		status, code = 404, "INVITATION_NOT_FOUND"
	case errors.Is(err, app.ErrCompanyConflict):
		status, code = 409, "COMPANY_CODE_CONFLICT"
	case errors.Is(err, app.ErrCompanyStatus):
		status, code = 409, "COMPANY_STATUS_CONFLICT"
	case errors.Is(err, app.ErrInvitationConflict):
		status, code = 409, "INVITATION_CONFLICT"
	case errors.Is(err, app.ErrLastAdmin):
		status, code = 409, "LAST_COMPANY_ADMIN"
	case errors.Is(err, app.ErrIdempotency):
		status, code = 409, "IDEMPOTENCY_CONFLICT"
	}
	httpapi.WriteError(w, r, &httpapi.Error{Status: status, Code: code})
}
func companyJSON(w http.ResponseWriter, r *http.Request, status int, value any, err error) {
	if err != nil {
		companyError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func decodeCompany(w http.ResponseWriter, r *http.Request, out any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return app.ErrInvalid
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return app.ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return app.ErrInvalid
	}
	return nil
}
func companyID(r *http.Request) string { return r.PathValue("id") }
func validCompanyIDs(r *http.Request, names ...string) bool {
	for _, name := range names {
		if !uuidPattern.MatchString(r.PathValue(name)) {
			return false
		}
	}
	return true
}
func (h Handler) listCompanies(w http.ResponseWriter, r *http.Request, _ string) {
	limit, offset := 20, 0
	var err error
	q := r.URL.Query()
	if q.Has("limit") {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			companyError(w, r, app.ErrInvalid)
			return
		}
	}
	if q.Has("offset") {
		offset, err = strconv.Atoi(q.Get("offset"))
		if err != nil {
			companyError(w, r, app.ErrInvalid)
			return
		}
	}
	f := app.CompanyFilter{Search: q.Get("search"), Status: q.Get("status"), PlanID: q.Get("plan_id"), Sort: q.Get("sort"), Limit: limit, Offset: offset}
	if f.PlanID != "" && !uuidPattern.MatchString(f.PlanID) {
		companyError(w, r, app.ErrInvalid)
		return
	}
	v, e := h.Companies.List(r.Context(), f)
	companyJSON(w, r, 200, v, e)
}
func (h Handler) getCompany(w http.ResponseWriter, r *http.Request, _ string) {
	if !validCompanyIDs(r, "id") {
		companyError(w, r, app.ErrInvalid)
		return
	}
	v, e := h.Companies.Get(r.Context(), companyID(r))
	companyJSON(w, r, 200, v, e)
}
func (h Handler) getCompanyUsage(w http.ResponseWriter, r *http.Request, _ string) {
	if !validCompanyIDs(r, "id") {
		companyError(w, r, app.ErrInvalid)
		return
	}
	v, e := h.Companies.Get(r.Context(), companyID(r))
	companyJSON(w, r, 200, map[string]any{"company_id": v.ID, "member_count": v.MemberCount, "project_count": v.ProjectCount, "last_activity_at": v.LastActivityAt}, e)
}
func (h Handler) createCompany(w http.ResponseWriter, r *http.Request, actor string) {
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 1 || len(key) > 128 {
		companyError(w, r, app.ErrInvalid)
		return
	}
	var in app.CompanyInput
	if err := decodeCompany(w, r, &in); err != nil {
		companyError(w, r, err)
		return
	}
	if in.PlanID != nil && !uuidPattern.MatchString(*in.PlanID) {
		companyError(w, r, app.ErrInvalid)
		return
	}
	v, e := h.Companies.Create(r.Context(), actor, key, in)
	companyJSON(w, r, 201, v, e)
}
func (h Handler) updateCompany(w http.ResponseWriter, r *http.Request, actor string) {
	if !validCompanyIDs(r, "id") {
		companyError(w, r, app.ErrInvalid)
		return
	}
	var in app.CompanyPatch
	if err := decodeCompany(w, r, &in); err != nil {
		companyError(w, r, err)
		return
	}
	if in.PlanID != nil && !uuidPattern.MatchString(*in.PlanID) {
		companyError(w, r, app.ErrInvalid)
		return
	}
	v, e := h.Companies.Update(r.Context(), actor, companyID(r), in)
	companyJSON(w, r, 200, v, e)
}
func (h Handler) suspendCompany(w http.ResponseWriter, r *http.Request, actor string) {
	if !validCompanyIDs(r, "id") {
		companyError(w, r, app.ErrInvalid)
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := decodeCompany(w, r, &in); err != nil {
		companyError(w, r, err)
		return
	}
	v, e := h.Companies.ChangeStatus(r.Context(), actor, companyID(r), "suspended", in.Reason)
	companyJSON(w, r, 200, v, e)
}
func (h Handler) reactivateCompany(w http.ResponseWriter, r *http.Request, actor string) {
	if !validCompanyIDs(r, "id") {
		companyError(w, r, app.ErrInvalid)
		return
	}
	v, e := h.Companies.ChangeStatus(r.Context(), actor, companyID(r), "active", "")
	companyJSON(w, r, 200, v, e)
}
func (h Handler) companyAdmins(w http.ResponseWriter, r *http.Request, _ string) {
	if !validCompanyIDs(r, "id") {
		companyError(w, r, app.ErrInvalid)
		return
	}
	v, e := h.Companies.Repository.Admins(r.Context(), companyID(r))
	companyJSON(w, r, 200, v, e)
}
func (h Handler) inviteAdmin(w http.ResponseWriter, r *http.Request, actor string) {
	if !validCompanyIDs(r, "id") {
		companyError(w, r, app.ErrInvalid)
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if err := decodeCompany(w, r, &in); err != nil {
		companyError(w, r, err)
		return
	}
	v, e := h.Companies.Invite(r.Context(), actor, companyID(r), in.Email)
	companyJSON(w, r, 201, v, e)
}
func (h Handler) resendAdmin(w http.ResponseWriter, r *http.Request, actor string) {
	if !validCompanyIDs(r, "id", "invitationId") {
		companyError(w, r, app.ErrInvalid)
		return
	}
	v, e := h.Companies.Repository.Resend(r.Context(), actor, companyID(r), r.PathValue("invitationId"))
	companyJSON(w, r, 200, v, e)
}
func (h Handler) cancelAdmin(w http.ResponseWriter, r *http.Request, actor string) {
	if !validCompanyIDs(r, "id", "invitationId") {
		companyError(w, r, app.ErrInvalid)
		return
	}
	if err := h.Companies.Repository.Cancel(r.Context(), actor, companyID(r), r.PathValue("invitationId")); err != nil {
		companyError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (h Handler) adminStatus(w http.ResponseWriter, r *http.Request, actor string) {
	if !validCompanyIDs(r, "id", "userId") {
		companyError(w, r, app.ErrInvalid)
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := decodeCompany(w, r, &in); err != nil {
		companyError(w, r, err)
		return
	}
	v, e := h.Companies.Repository.SetAdminStatus(r.Context(), actor, companyID(r), r.PathValue("userId"), in.Status)
	companyJSON(w, r, 200, v, e)
}
