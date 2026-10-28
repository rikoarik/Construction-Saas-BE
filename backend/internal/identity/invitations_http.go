package identity

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"construction-saas/backend/internal/httpapi"
)

const tenancyBodyLimit = 8 * 1024

func registerTenancyRoutes(mux *http.ServeMux, service *AuthService, secureCookie bool) {
	mux.HandleFunc("POST /api/v1/auth/invitations/accept", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		var session *Session
		if cookie, err := r.Cookie(service.cookieName()); err == nil {
			if current, err := service.CurrentUser(r.Context(), cookie); err == nil && current.AuthState == "active" {
				session = &current
			}
		}
		user, err := service.AcceptInvitation(r.Context(), input.Token, input.Password, session, clientInfo(r))
		if err != nil {
			writeTenancyError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true, "user_id": user.ID})
	})
	mux.HandleFunc("GET /api/v1/companies/{companyId}/invitations", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		companyID := r.PathValue("companyId")
		if !sessionIDPattern.MatchString(companyID) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		invitations, err := service.ListInvitations(r.Context(), session.UserID, companyID)
		if err != nil {
			writeTenancyError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"invitations": invitations})
	})
	mux.HandleFunc("POST /api/v1/companies/{companyId}/invitations", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		companyID := r.PathValue("companyId")
		if !sessionIDPattern.MatchString(companyID) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		var input struct {
			Email string `json:"email"`
			Role  string `json:"role"`
		}
		if !decodeLimitedJSON(w, r, &input, tenancyBodyLimit) {
			return
		}
		invitation, err := service.InviteMember(r.Context(), session.UserID, companyID, input.Email, input.Role, clientInfo(r))
		if err != nil {
			writeTenancyError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(invitation)
	})
	mux.HandleFunc("DELETE /api/v1/companies/{companyId}/invitations/{invitationId}", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		companyID, invitationID := r.PathValue("companyId"), r.PathValue("invitationId")
		if !sessionIDPattern.MatchString(companyID) || !sessionIDPattern.MatchString(invitationID) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		if err := service.RevokeInvitation(r.Context(), session.UserID, companyID, invitationID); err != nil {
			writeTenancyError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/companies/{companyId}/members", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		companyID := r.PathValue("companyId")
		if !sessionIDPattern.MatchString(companyID) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		members, err := service.ListMembers(r.Context(), session.UserID, companyID)
		if err != nil {
			writeTenancyError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"members": members})
	})
	mux.HandleFunc("PATCH /api/v1/companies/{companyId}/members/{userId}", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		companyID, targetID := r.PathValue("companyId"), r.PathValue("userId")
		if !sessionIDPattern.MatchString(companyID) || !sessionIDPattern.MatchString(targetID) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		var input struct {
			Role   *string `json:"role"`
			Status *string `json:"status"`
		}
		if !decodeLimitedJSON(w, r, &input, tenancyBodyLimit) {
			return
		}
		err := service.UpdateMembership(r.Context(), session.UserID, companyID, targetID, input.Role, input.Status)
		if err != nil {
			writeTenancyError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func writeTenancyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrCompanyAccess), errors.Is(err, ErrForbidden), errors.Is(err, ErrCapability):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusForbidden, Code: "FORBIDDEN"})
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusNotFound, Code: "NOT_FOUND"})
	case errors.Is(err, ErrInvitationExists):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusConflict, Code: "INVITATION_EXISTS"})
	case errors.Is(err, ErrInvitationInvalid):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "INVALID_INVITATION"})
	case errors.Is(err, ErrInvitationRole):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "INVALID_ROLE"})
	case errors.Is(err, ErrAccountRequired):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusConflict, Code: "ACCOUNT_REQUIRED"})
	case errors.Is(err, ErrAuthRequired):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "AUTHENTICATION_REQUIRED"})
	case errors.Is(err, ErrLastOwner):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusConflict, Code: "LAST_OWNER"})
	case errors.Is(err, ErrSelfManagement):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusForbidden, Code: "SELF_MANAGEMENT_FORBIDDEN"})
	case errors.Is(err, ErrPasswordPolicy):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "PASSWORD_POLICY"})
	default:
		httpapi.WriteError(w, r, err)
	}
}

func decodeLimitedJSON(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	if r.ContentLength > limit {
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
		return false
	}
	return true
}
