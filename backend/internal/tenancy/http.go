package tenancy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"construction-saas/backend/internal/httpapi"
	"construction-saas/backend/internal/identity"
)

type Authenticator interface {
	CurrentUser(context.Context, *http.Cookie) (identity.Session, error)
}
type Handler struct {
	Store *Store
	Auth  Authenticator
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/companies", h.createCompany)
	mux.HandleFunc("GET /api/v1/companies", h.listCompanies)
	mux.HandleFunc("GET /api/v1/companies/{companyID}/members", h.listMembers)
	mux.HandleFunc("POST /api/v1/companies/{companyID}/invitations", h.createInvitation)
	mux.HandleFunc("GET /api/v1/companies/{companyID}/invitations", h.listInvitations)
}
func (h *Handler) userID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.Auth == nil || h.Store == nil {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 500, Code: "INTERNAL_ERROR"})
		return "", false
	}
	cookie, err := r.Cookie(identity.SessionCookieName)
	if err != nil {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 401, Code: "UNAUTHENTICATED"})
		return "", false
	}
	session, err := h.Auth.CurrentUser(r.Context(), cookie)
	if err != nil {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 401, Code: "UNAUTHENTICATED"})
		return "", false
	}
	return session.UserID, true
}
func (h *Handler) createCompany(w http.ResponseWriter, r *http.Request) {
	user, ok := h.userID(w, r)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if decodeJSON(w, r, &in) != nil || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Slug) == "" {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 400, Code: "VALIDATION_ERROR"})
		return
	}
	item, err := h.Store.CreateCompany(r.Context(), user, in.Name, in.Slug)
	if err != nil {
		writeCompanyError(w, r, err)
		return
	}
	writeJSON(w, 201, item)
}
func (h *Handler) listCompanies(w http.ResponseWriter, r *http.Request) {
	user, ok := h.userID(w, r)
	if !ok {
		return
	}
	items, err := h.Store.ListCompanies(r.Context(), user)
	if err != nil {
		httpapi.WriteError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
}
func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	user, ok := h.userID(w, r)
	if !ok {
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil {
			httpapi.WriteError(w, r, &httpapi.Error{Status: 400, Code: "VALIDATION_ERROR"})
			return
		}
		limit = n
	}
	items, err := h.Store.ListMembers(r.Context(), r.PathValue("companyID"), user, limit)
	if err != nil {
		writeCompanyError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
}
func (h *Handler) createInvitation(w http.ResponseWriter, r *http.Request) {
	user, ok := h.userID(w, r)
	if !ok {
		return
	}
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if decodeJSON(w, r, &in) != nil || strings.TrimSpace(in.Email) == "" || in.Role == "" {
		httpapi.WriteError(w, r, &httpapi.Error{Status: 400, Code: "VALIDATION_ERROR"})
		return
	}
	item, err := h.Store.CreateInvitation(r.Context(), user, r.PathValue("companyID"), in.Email, in.Role, 7*24*time.Hour)
	if err != nil {
		writeCompanyError(w, r, err)
		return
	}
	writeJSON(w, 201, item)
}
func (h *Handler) listInvitations(w http.ResponseWriter, r *http.Request) {
	user, ok := h.userID(w, r)
	if !ok {
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil {
			httpapi.WriteError(w, r, &httpapi.Error{Status: 400, Code: "VALIDATION_ERROR"})
			return
		}
		limit = n
	}
	items, err := h.Store.ListInvitations(r.Context(), user, r.PathValue("companyID"), limit)
	if err != nil {
		writeCompanyError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.ContentLength > 16*1024 {
		return errors.New("body too large")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	d.DisallowUnknownFields()
	if e := d.Decode(dst); e != nil {
		return e
	}
	var trailing any
	if e := d.Decode(&trailing); !errors.Is(e, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}
func writeCompanyError(w http.ResponseWriter, r *http.Request, e error) {
	switch {
	case errors.Is(e, ErrInvalid):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 400, Code: "VALIDATION_ERROR"})
	case errors.Is(e, ErrNotFound):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 404, Code: "NOT_FOUND"})
	case errors.Is(e, ErrForbidden):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: "FORBIDDEN"})
	case errors.Is(e, ErrConflict):
		httpapi.WriteError(w, r, &httpapi.Error{Status: 409, Code: "CONFLICT"})
	default:
		httpapi.WriteError(w, r, e)
	}
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
