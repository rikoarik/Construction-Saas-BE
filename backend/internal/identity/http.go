package identity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"construction-saas/backend/internal/httpapi"
)

const loginBodyLimit = 16 * 1024

func RegisterAuthRoutes(mux *http.ServeMux, service *AuthService, secureCookie bool) {
	registerSessionRoutes(mux, service, secureCookie)
	registerPasswordChange(mux, service, secureCookie)
	registerMFARoutes(mux, service, secureCookie)
	registerRecoveryRoutes(mux, service, secureCookie)
	registerTenancyRoutes(mux, service, secureCookie)
	mux.HandleFunc("GET /api/v1/platform/session", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		cookie, err := r.Cookie(service.cookieName())
		if err != nil {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"})
			return
		}
		session, err := service.CurrentUser(r.Context(), cookie)
		if err != nil {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"})
			return
		}
		if err := service.PlatformSession(r.Context(), session); err != nil {
			writeAuthError(w, r, err)
			return
		}
		capabilities, err := service.Store.PlatformCapabilities(r.Context(), session.UserID)
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authenticated": true,
			"mfa_verified":  true,
			"capabilities":  capabilities,
		})
	})
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > loginBodyLimit {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		var credentials Credentials
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, loginBodyLimit))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&credentials); err != nil {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		result, err := service.LoginWithClient(r.Context(), credentials, clientInfo(r))
		if err != nil {
			writeAuthError(w, r, err)
			return
		}
		service.SetSessionCookie(w, result.Token, result.ExpiresAt, secureCookie)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]string{"next": result.Next})
	})
	mux.HandleFunc("POST /api/v1/auth/logout-all", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(service.cookieName())
		if err != nil {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"})
			return
		}
		session, err := service.CurrentUser(r.Context(), cookie)
		if err != nil {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"})
			return
		}
		if err := service.Store.RevokeAllSessions(r.Context(), session.UserID); err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		service.ClearSessionCookie(w, secureCookie)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(service.cookieName())
		if err != nil {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"})
			return
		}
		session, err := service.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"})
			return
		}
		if err := service.Logout(r.Context(), session.UserID, cookie.Value); err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		service.ClearSessionCookie(w, secureCookie)
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *AuthService) cookieName() string {
	if s.CookieName == "" {
		return SessionCookieName
	}
	return s.CookieName
}

// CurrentUser resolves an opaque browser cookie to its live session, including
// mfa_pending sessions. Callers must enforce the required auth state.
func (s *AuthService) CurrentUser(ctx context.Context, cookie *http.Cookie) (Session, error) {
	if cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		return Session{}, ErrUnauthenticated
	}
	return s.Authenticate(ctx, cookie.Value)
}
