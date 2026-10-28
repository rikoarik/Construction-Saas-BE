package identity

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"construction-saas/backend/internal/httpapi"
)

const mfaRequestBodyLimit = 8 * 1024

// MFARoutes registers the privileged-authentication endpoints. All of them
// require an existing mfa_pending session except for status discovery.
func registerMFARoutes(mux *http.ServeMux, service *AuthService, secureCookie bool) {
	mux.HandleFunc("POST /api/v1/auth/mfa/enrollment", func(w http.ResponseWriter, r *http.Request) {
		session, ok := pendingSession(w, r, service)
		if !ok {
			return
		}
		enrollment, err := service.StartMFAEnrollment(r.Context(), session, clientInfo(r))
		if err != nil {
			writeAuthError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(enrollment)
	})
	mux.HandleFunc("POST /api/v1/auth/mfa/enrollment/confirm", func(w http.ResponseWriter, r *http.Request) {
		session, ok := pendingSession(w, r, service)
		if !ok {
			return
		}
		code, ok := decodeMFACode(w, r)
		if !ok {
			return
		}
		result, err := service.ConfirmMFAEnrollment(r.Context(), session, code, clientInfo(r))
		if err != nil {
			writeAuthError(w, r, err)
			return
		}
		service.SetSessionCookie(w, result.Token, result.ExpiresAt, secureCookie)
		writeMFAJSON(w, result)
	})
	mux.HandleFunc("POST /api/v1/auth/mfa/challenge", func(w http.ResponseWriter, r *http.Request) {
		session, ok := pendingSession(w, r, service)
		if !ok {
			return
		}
		code, ok := decodeMFACode(w, r)
		if !ok {
			return
		}
		result, err := service.VerifyMFAChallenge(r.Context(), session, code, clientInfo(r))
		if err != nil {
			writeAuthError(w, r, err)
			return
		}
		service.SetSessionCookie(w, result.Token, result.ExpiresAt, secureCookie)
		writeMFAJSON(w, result)
	})
	mux.HandleFunc("POST /api/v1/auth/mfa/recovery", func(w http.ResponseWriter, r *http.Request) {
		session, ok := pendingSession(w, r, service)
		if !ok {
			return
		}
		code, ok := decodeMFACode(w, r)
		if !ok {
			return
		}
		result, err := service.VerifyMFARecovery(r.Context(), session, code, clientInfo(r))
		if err != nil {
			writeAuthError(w, r, err)
			return
		}
		service.SetSessionCookie(w, result.Token, result.ExpiresAt, secureCookie)
		writeMFAJSON(w, result)
	})
}

func pendingSession(w http.ResponseWriter, r *http.Request, service *AuthService) (Session, bool) {
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
	if session.AuthState != "mfa_pending" {
		// An already verified session has nothing to prove; treat repeated
		// challenges as a conflict rather than silently re-authenticating.
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusConflict, Code: "MFA_ALREADY_VERIFIED"})
		return Session{}, false
	}
	return session, true
}

func decodeMFACode(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.ContentLength > mfaRequestBodyLimit {
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
		return "", false
	}
	var input struct {
		Code string `json:"code"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, mfaRequestBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
		return "", false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
		return "", false
	}
	code := strings.TrimSpace(input.Code)
	if code == "" || len(code) > 64 {
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "VALIDATION_ERROR"})
		return "", false
	}
	return code, true
}

func writeMFAJSON(w http.ResponseWriter, result MFAResult) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(result)
}

func writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var limited *RateLimitError
	switch {
	case errors.As(err, &limited):
		seconds := int(limited.RetryAfter.Seconds())
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusTooManyRequests, Code: "RATE_LIMITED"})
	case errors.Is(err, ErrUnauthenticated):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"})
	case errors.Is(err, ErrMFARequired):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusForbidden, Code: "MFA_REQUIRED"})
	case errors.Is(err, ErrMFANotEnrolled):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusConflict, Code: "MFA_NOT_ENROLLED"})
	case errors.Is(err, ErrMFAAlreadyEnrolled):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusConflict, Code: "MFA_ALREADY_ENROLLED"})
	case errors.Is(err, ErrMFAInvalidCode):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "INVALID_MFA_CODE"})
	case errors.Is(err, ErrMFAReplay):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "MFA_CODE_REPLAYED"})
	case errors.Is(err, ErrMFAExpired):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusUnauthorized, Code: "MFA_EXPIRED"})
	case errors.Is(err, ErrMFALocked):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusTooManyRequests, Code: "MFA_LOCKED"})
	case errors.Is(err, ErrMFAUnavailable):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusServiceUnavailable, Code: "MFA_NOT_AVAILABLE"})
	case errors.Is(err, ErrForbidden):
		httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusForbidden, Code: "FORBIDDEN"})
	default:
		httpapi.WriteError(w, r, err)
	}
}

func clientInfo(r *http.Request) ClientInfo {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	}
	return ClientInfo{IP: ip, UserAgent: r.UserAgent()}
}
