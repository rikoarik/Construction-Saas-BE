package platformhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"construction-saas/backend/internal/httpapi"
	"construction-saas/backend/internal/identity"
	"construction-saas/backend/internal/platform/app"
)

type Handler struct {
	Auth      *identity.AuthService
	Service   app.Service
	Companies app.CompanyManagement
}

func (h Handler) Register(mux *http.ServeMux) {
	h.registerCompanies(mux)
	if h.Companies.Repository == nil {
		mux.HandleFunc("GET /api/v1/platform/companies", h.protect(h.companies))
	}
	for path, fn := range map[string]func(http.ResponseWriter, *http.Request){
		"/api/v1/platform/me":                                  h.me,
		"/api/v1/platform/dashboard":                           h.dashboard,
		"/api/v1/platform/dashboard/company-growth":            h.growth,
		"/api/v1/platform/dashboard/company-status":            h.status,
		"/api/v1/platform/dashboard/activities":                h.activities,
		"/api/v1/platform/dashboard/subscription-distribution": h.distribution,
		"/api/v1/platform/dashboard/storage-usage":             h.storage,
		"/api/v1/platform/dashboard/integration-health":        h.integrations,
		"/api/v1/platform/jobs":                                h.jobs,
		"/api/v1/platform/notifications":                       h.notifications,
	} {
		mux.HandleFunc("GET "+path, h.protect(fn))
	}
	// Read markers are a mutation of platform metadata only: they never touch
	// tenant content, and they are scoped to the authenticated operator.
	for path, fn := range map[string]func(http.ResponseWriter, *http.Request){
		"/api/v1/platform/notifications/read":     h.notificationsRead,
		"/api/v1/platform/notifications/unread":   h.notificationsUnread,
		"/api/v1/platform/notifications/read-all": h.notificationsReadAll,
	} {
		mux.HandleFunc("POST "+path, h.protect(fn))
	}
}

func (h Handler) protect(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
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
			if errors.Is(err, identity.ErrMFARequired) {
				httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: "MFA_REQUIRED"})
				return
			}
			if errors.Is(err, identity.ErrForbidden) {
				httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: "PLATFORM_PERMISSION_DENIED"})
				return
			}
			httpapi.WriteError(w, r, err)
			return
		}
		capabilities, err := h.Auth.Store.PlatformCapabilities(r.Context(), session.UserID)
		if err != nil {
			httpapi.WriteError(w, r, err)
			return
		}
		allowed := false
		for _, c := range capabilities {
			if c == "platform.dashboard.read" {
				allowed = true
				break
			}
		}
		if !allowed {
			httpapi.WriteError(w, r, &httpapi.Error{Status: 403, Code: "PLATFORM_PERMISSION_DENIED"})
			return
		}
		// Handlers read the operator from the verified session, never from the
		// request, so read markers cannot be written for another account.
		next(w, r.WithContext(app.WithOperator(r.Context(), session.UserID)))
	}
}
func respond(w http.ResponseWriter, r *http.Request, value any, err error) {
	if err != nil {
		if errors.Is(err, app.ErrInvalid) {
			httpapi.WriteError(w, r, &httpapi.Error{Status: 400, Code: "VALIDATION_ERROR"})
		} else {
			httpapi.WriteError(w, r, err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func (h Handler) me(w http.ResponseWriter, r *http.Request) {
	// /platform/session remains the authentication check for existing clients; /me
	// adds the operator ID for dashboard personalization without exposing PII.
	cookie, _ := r.Cookie(identity.SessionCookieName)
	session, err := h.Auth.CurrentUser(r.Context(), cookie)
	if err != nil {
		respond(w, r, nil, err)
		return
	}
	caps, err := h.Auth.Store.PlatformCapabilities(r.Context(), session.UserID)
	respond(w, r, struct {
		UserID       string   `json:"user_id"`
		Capabilities []string `json:"capabilities"`
		MFAVerified  bool     `json:"mfa_verified"`
	}{session.UserID, caps, true}, err)
}
func (h Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	v, e := h.Service.Dashboard(r.Context(), r.URL.Query().Get("range"))
	respond(w, r, v, e)
}
func (h Handler) growth(w http.ResponseWriter, r *http.Request) {
	v, e := h.Service.Growth(r.Context(), r.URL.Query().Get("range"))
	respond(w, r, v, e)
}
func (h Handler) status(w http.ResponseWriter, r *http.Request) {
	v, e := h.Service.Status(r.Context())
	respond(w, r, v, e)
}
func (h Handler) distribution(w http.ResponseWriter, r *http.Request) {
	v, e := h.Service.Distribution(r.Context())
	respond(w, r, v, e)
}
func (h Handler) storage(w http.ResponseWriter, r *http.Request) {
	v, e := h.Service.Storage(r.Context())
	respond(w, r, v, e)
}
func (h Handler) integrations(w http.ResponseWriter, r *http.Request) {
	v, e := h.Service.IntegrationHealth(r.Context())
	respond(w, r, v, e)
}
func page(r *http.Request, defaultLimit int) (int, int, error) {
	limit, offset := defaultLimit, 0
	var err error
	q := r.URL.Query()
	if q.Has("limit") {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			return 0, 0, app.ErrInvalid
		}
	}
	if q.Has("offset") {
		offset, err = strconv.Atoi(q.Get("offset"))
		if err != nil {
			return 0, 0, app.ErrInvalid
		}
	}
	return limit, offset, nil
}
func (h Handler) companies(w http.ResponseWriter, r *http.Request) {
	if sort := r.URL.Query().Get("sort"); sort != "" && sort != "-created_at" {
		respond(w, r, nil, app.ErrInvalid)
		return
	}
	l, o, e := page(r, 5)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	v, e := h.Service.Companies(r.Context(), l, o)
	respond(w, r, v, e)
}
func (h Handler) jobs(w http.ResponseWriter, r *http.Request) {
	l, o, e := page(r, 5)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	statuses := []string{"failed", "retry_pending"}
	if r.URL.Query().Has("status") {
		statuses = strings.Split(r.URL.Query().Get("status"), ",")
	}
	v, e := h.Service.Jobs(r.Context(), statuses, l, o)
	respond(w, r, v, e)
}
func (h Handler) activities(w http.ResponseWriter, r *http.Request) {
	l, o, e := page(r, 10)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	v, e := h.Service.Activities(r.Context(), l, o)
	respond(w, r, v, e)
}

func (h Handler) notifications(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if r.URL.Query().Has("limit") {
		value, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil {
			respond(w, r, nil, app.ErrInvalid)
			return
		}
		limit = value
	}
	v, e := h.Service.Notifications(r.Context(), app.OperatorID(r.Context()), app.NotificationFilter{
		Type: r.URL.Query().Get("type"),
		Read: r.URL.Query().Get("read"),
	}, limit)
	respond(w, r, v, e)
}

// notificationKeys decodes the shared mark-read body. The body is bounded because
// keys are client-supplied identifiers.
func notificationKeys(w http.ResponseWriter, r *http.Request) ([]string, error) {
	var body struct {
		Keys []string `json:"keys"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	if err := decoder.Decode(&body); err != nil {
		return nil, app.ErrInvalid
	}
	return body.Keys, nil
}

func (h Handler) markNotifications(read bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keys, err := notificationKeys(w, r)
		if err != nil {
			respond(w, r, nil, err)
			return
		}
		if err := h.Service.MarkNotificationsRead(r.Context(), app.OperatorID(r.Context()), keys, read); err != nil {
			respond(w, r, nil, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h Handler) notificationsRead(w http.ResponseWriter, r *http.Request) {
	h.markNotifications(true)(w, r)
}

func (h Handler) notificationsUnread(w http.ResponseWriter, r *http.Request) {
	h.markNotifications(false)(w, r)
}

func (h Handler) notificationsReadAll(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.MarkAllNotificationsRead(r.Context(), app.OperatorID(r.Context())); err != nil {
		respond(w, r, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
