package identity

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"construction-saas/backend/internal/httpapi"
)

func registerPasswordChange(mux *http.ServeMux, service *AuthService, secureCookie bool) {
	mux.HandleFunc("POST /api/v1/auth/change-password", func(w http.ResponseWriter, r *http.Request) {
		session, ok := authenticatedAccount(w, r, service)
		if !ok {
			return
		}
		var input struct {
			Current string `json:"current_password"`
			New     string `json:"new_password"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			httpapi.WriteError(w, r, &httpapi.Error{Status: 400, Code: "VALIDATION_ERROR"})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: 400, Code: "VALIDATION_ERROR"})
			return
		}
		err := service.ChangePassword(r.Context(), session, input.Current, input.New)
		if errors.Is(err, ErrPasswordPolicy) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: 400, Code: "VALIDATION_ERROR"})
			return
		}
		if errors.Is(err, ErrUnauthenticated) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: 401, Code: "UNAUTHENTICATED"})
			return
		}
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		service.ClearSessionCookie(w, secureCookie)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	})
}
