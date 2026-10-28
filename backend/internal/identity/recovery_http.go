package identity

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"construction-saas/backend/internal/httpapi"
)

const recoveryBodyLimit = 8 * 1024

func registerRecoveryRoutes(mux *http.ServeMux, service *AuthService, secureCookie bool) {
	mux.HandleFunc("POST /api/v1/auth/forgot-password", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Email string `json:"email"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := service.RequestPasswordReset(r.Context(), input.Email, clientInfo(r)); err != nil {
			var limited *RateLimitError
			if errors.As(err, &limited) {
				writeAuthError(w, r, err)
				return
			}
			httpapi.WriteError(w, r, err)
			return
		}
		accepted(w)
	})
	mux.HandleFunc("POST /api/v1/auth/reset-password", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token       string `json:"token"`
			NewPassword string `json:"new_password"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		err := service.ResetPassword(r.Context(), input.Token, input.NewPassword, clientInfo(r))
		switch {
		case errors.Is(err, ErrAuthTokenInvalid):
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "INVALID_TOKEN"})
		case errors.Is(err, ErrPasswordPolicy):
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "PASSWORD_POLICY"})
		case err != nil:
			httpapi.WriteError(w, r, err)
		default:
			service.ClearSessionCookie(w, secureCookie)
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("POST /api/v1/auth/verify-email", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token string `json:"token"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		err := service.VerifyEmail(r.Context(), input.Token, clientInfo(r))
		if errors.Is(err, ErrAuthTokenInvalid) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: http.StatusBadRequest, Code: "INVALID_TOKEN"})
			return
		}
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/auth/resend-verification", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Email string `json:"email"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if err := service.ResendVerification(r.Context(), input.Email, clientInfo(r)); err != nil {
			var limited *RateLimitError
			if errors.As(err, &limited) {
				writeAuthError(w, r, err)
				return
			}
			httpapi.WriteError(w, r, err)
			return
		}
		accepted(w)
	})
}

func accepted(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, recoveryBodyLimit))
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
