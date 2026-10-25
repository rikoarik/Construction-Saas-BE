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

func RegisterAuthRoutes(mux *http.ServeMux, service *AuthService, secureCookie bool) {
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 16*1024 {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
			return
		}
		var credentials Credentials
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
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
		token, expires, err := service.Login(r.Context(), credentials)
		if err != nil {
			if errors.Is(err, ErrUnauthenticated) {
				httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"})
				return
			}
			httpapi.WriteError(w, r, err)
			return
		}
		service.SetSessionCookie(w, token, expires, secureCookie)
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

// CurrentUser resolves an opaque browser cookie to its active session.
func (s *AuthService) CurrentUser(ctx context.Context, cookie *http.Cookie) (Session, error) {
	if cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		return Session{}, ErrUnauthenticated
	}
	return s.Authenticate(ctx, cookie.Value)
}
