package identity

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"

	"construction-saas/backend/internal/httpapi"
)

var sessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// authenticatedAccount rejects non-active sessions. A platform administrator
// reaching this point has already completed MFA, which is recorded on the
// session; a plain platform grant is never sufficient on its own.
func authenticatedAccount(w http.ResponseWriter, r *http.Request, service *AuthService) (Session, bool) {
	cookie, err := r.Cookie(service.cookieName())
	if err != nil {
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"})
		return Session{}, false
	}
	session, err := service.CurrentUser(r.Context(), cookie)
	if err != nil {
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"})
		return Session{}, false
	}
	if session.AuthState != "active" {
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusForbidden, Code: "MFA_REQUIRED"})
		return Session{}, false
	}
	return session, true
}

func registerSessionRoutes(mux *http.ServeMux, service *AuthService, secureCookie bool) {
	mux.HandleFunc("POST /api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		token, expires, err := service.RotateSession(r.Context(), session)
		if errors.Is(err, ErrUnauthenticated) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"})
			return
		}
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		service.SetSessionCookie(w, token, expires, secureCookie)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		account, err := service.Store.Account(r.Context(), session.UserID)
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		company, err := service.Store.SelectedCompany(r.Context(), session)
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			AccountInfo
			SelectedCompany *CompanyAccess `json:"selected_company"`
		}{account, company})
	})
	mux.HandleFunc("GET /api/v1/auth/sessions", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		sessions, err := service.Store.Sessions(r.Context(), session.UserID, session.ID)
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
	})
	mux.HandleFunc("DELETE /api/v1/auth/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		id := r.PathValue("id")
		if !sessionIDPattern.MatchString(id) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		revoked, err := service.Store.RevokeSession(r.Context(), session.UserID, id)
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		if !revoked {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusNotFound, Code: "NOT_FOUND"})
			return
		}
		if id == session.ID {
			service.ClearSessionCookie(w, secureCookie)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/auth/companies/select", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		var input struct {
			CompanyID string `json:"company_id"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || !sessionIDPattern.MatchString(input.CompanyID) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		err := service.Store.SelectCompany(r.Context(), session, input.CompanyID)
		if errors.Is(err, ErrCompanyAccess) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusForbidden, Code: "FORBIDDEN"})
			return
		}
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/v1/auth/companies", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		companies, err := service.Store.Companies(r.Context(), session.UserID)
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"companies": companies})
	})
}
