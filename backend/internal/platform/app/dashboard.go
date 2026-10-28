package app

import (
	"context"
	"errors"
	"regexp"
	"time"
)

var ErrInvalid = errors.New("invalid dashboard query")

type Period struct {
	Range string    `json:"range"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// PeriodFor uses half-open UTC intervals. End is the instant at which the request is evaluated.
func PeriodFor(value string, now time.Time) (Period, error) {
	now = now.UTC()
	if value == "" {
		value = "30d"
	}
	var start time.Time
	switch value {
	case "7d":
		start = now.AddDate(0, 0, -7)
	case "30d":
		start = now.AddDate(0, 0, -30)
	case "90d":
		start = now.AddDate(0, 0, -90)
	case "12m":
		start = now.AddDate(-1, 0, 0)
	default:
		return Period{}, ErrInvalid
	}
	return Period{Range: value, Start: start, End: now}, nil
}

type Metric struct {
	Value    *int64 `json:"value"`
	Previous *int64 `json:"previous"`
	// Series holds one point per UTC bucket of the selected period, oldest first,
	// so the dashboard can draw a trend without a second request. Nil means the
	// history is not reconstructable from stored data and must not be drawn.
	Series []int64 `json:"series"`
}

type Dashboard struct {
	Period         Period `json:"period"`
	Companies      Metric `json:"companies"`
	Users          Metric `json:"users"`
	ActiveProjects Metric `json:"active_projects"`
	FailedJobs     Metric `json:"failed_jobs"`
}

type GrowthPoint struct {
	Start          time.Time `json:"start"`
	NewCompanies   int64     `json:"new_companies"`
	TotalCompanies int64     `json:"total_companies"`
}

type Growth struct {
	Period Period        `json:"period"`
	Points []GrowthPoint `json:"points"`
}

type Status struct {
	Active           int64   `json:"active"`
	Suspended        int64   `json:"suspended"`
	Archived         int64   `json:"archived"`
	Total            int64   `json:"total"`
	ActivePercent    float64 `json:"active_percent"`
	SuspendedPercent float64 `json:"suspended_percent"`
	ArchivedPercent  float64 `json:"archived_percent"`
}

// WithPercentages uses the same confirmed total for every status.
func (s Status) WithPercentages() Status {
	s.ActivePercent, s.SuspendedPercent, s.ArchivedPercent = 0, 0, 0
	if s.Total > 0 {
		s.ActivePercent = 100 * float64(s.Active) / float64(s.Total)
		s.SuspendedPercent = 100 * float64(s.Suspended) / float64(s.Total)
		s.ArchivedPercent = 100 * float64(s.Archived) / float64(s.Total)
	}
	return s
}

type Company struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	SubscriptionPlan *string   `json:"subscription_plan"`
	MemberCount      int64     `json:"member_count"`
	ProjectCount     *int64    `json:"project_count"`
}

type Job struct {
	ID            int64      `json:"id"`
	CompanyID     *string    `json:"company_id"`
	CompanyName   *string    `json:"company_name"`
	Type          string     `json:"type"`
	Status        string     `json:"status"`
	ErrorCode     *string    `json:"error_code"`
	CreatedAt     time.Time  `json:"created_at"`
	FailedAt      *time.Time `json:"failed_at"`
	AvailableAt   time.Time  `json:"available_at"`
	Attempts      int        `json:"attempts"`
	RetryEligible bool       `json:"retry_eligible"`
}

type Activity struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	CompanyID  *string   `json:"company_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

type Page[T any] struct {
	Items   []T  `json:"items"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

type Distribution struct {
	Items     []PlanCount `json:"items"`
	Available bool        `json:"available"`
}

type PlanCount struct {
	Plan      string `json:"plan"`
	Companies int64  `json:"companies"`
}

type Storage struct {
	UsedBytes  *int64 `json:"used_bytes"`
	QuotaBytes *int64 `json:"quota_bytes"`
	QuotaState string `json:"quota_state"`
	// APIRequests is nil until the platform has recorded at least one request;
	// an empty counter is unavailable data, not a confirmed zero.
	APIRequests *APIRequestUsage `json:"api_requests"`
}

// APIRequestUsage is requests measured in the current UTC calendar month.
// Used is nil until the platform has recorded any request at all: unmetered
// traffic is unavailable, not a confirmed zero. Limit comes from platform
// configuration and is nil when no monthly quota is set, in which case there is
// no denominator and no percentage to report.
type APIRequestUsage struct {
	Used  *int64 `json:"used"`
	Limit *int64 `json:"limit"`
}

type Integration struct {
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	CheckedAt *time.Time `json:"checked_at"`
}

// Notification is one entry of the operator's header feed. Type, severity, and
// event tokens are machine values; every human-readable string is built in the
// frontend locale so the backend never ships prose.
type Notification struct {
	Key         string    `json:"key"`
	Type        string    `json:"type"`
	Severity    string    `json:"severity"`
	CompanyID   *string   `json:"company_id"`
	CompanyName *string   `json:"company_name"`
	EventType   *string   `json:"event_type"`
	ErrorCode   *string   `json:"error_code"`
	Plan        *string   `json:"plan"`
	OccurredAt  time.Time `json:"occurred_at"`
	Read        bool      `json:"read"`
}

// NotificationPage always reports the operator's total unread count, independent
// of the active filter, so the bell badge stays correct while a filter is applied.
type NotificationPage struct {
	Items  []Notification `json:"items"`
	Unread int64          `json:"unread"`
	Type   string         `json:"type"`
	Read   string         `json:"read"`
}

// NotificationFilter narrows the feed. Empty values mean "everything"; Type
// groups the concrete notification types so the UI cannot invent one.
type NotificationFilter struct {
	Type string
	Read string
}

var notificationKeyPattern = regexp.MustCompile(`^(process|company|subscription):[A-Za-z0-9-]{1,64}$`)

// MaxNotificationKeys bounds one mark-read request so a single call cannot write
// an unbounded number of markers.
const MaxNotificationKeys = 100

// types maps a filter group to the concrete notification types it selects.
func (f NotificationFilter) types() ([]string, error) {
	switch f.Type {
	case "":
		return nil, nil
	case "process":
		return []string{"process_failed", "process_retry"}, nil
	case "company":
		return []string{"company_suspended"}, nil
	case "subscription":
		return []string{"subscription_past_due"}, nil
	default:
		return nil, ErrInvalid
	}
}

func (f NotificationFilter) validRead() bool {
	return f.Read == "" || f.Read == "unread" || f.Read == "read"
}

type Repository interface {
	Dashboard(context.Context, Period) (Dashboard, error)
	Growth(context.Context, Period) (Growth, error)
	Status(context.Context) (Status, error)
	Companies(context.Context, int, int) (Page[Company], error)
	Jobs(context.Context, []string, int, int) (Page[Job], error)
	Activities(context.Context, int, int) (Page[Activity], error)
	Distribution(context.Context) (Distribution, error)
	IntegrationHealth(context.Context) ([]Integration, error)
	Storage(context.Context) (Storage, error)
	Notifications(context.Context, string, []string, string, int) (NotificationPage, error)
	MarkNotifications(context.Context, string, []string, bool) error
	MarkAllNotificationsRead(context.Context, string) error
}

type Service struct {
	Read Repository
	Now  func() time.Time
	// APIRequestQuota is the platform-wide monthly request limit shown as the
	// denominator of the usage bar. Nil means unconfigured, so no bar is drawn.
	APIRequestQuota *int64
	// StorageQuota is configured independently from measured storage usage.
	StorageQuota *int64
}

func (s Service) period(value string) (Period, error) {
	if s.Now != nil {
		return PeriodFor(value, s.Now())
	}
	return PeriodFor(value, time.Now())
}
func (s Service) Dashboard(ctx context.Context, value string) (Dashboard, error) {
	p, err := s.period(value)
	if err != nil {
		return Dashboard{}, err
	}
	return s.Read.Dashboard(ctx, p)
}
func (s Service) Growth(ctx context.Context, value string) (Growth, error) {
	if value == "" {
		value = "12m"
	}
	p, err := s.period(value)
	if err != nil {
		return Growth{}, err
	}
	return s.Read.Growth(ctx, p)
}
func (s Service) Status(ctx context.Context) (Status, error) {
	status, err := s.Read.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	return status.WithPercentages(), nil
}
func (s Service) Companies(ctx context.Context, limit, offset int) (Page[Company], error) {
	if !validPage(limit, offset) {
		return Page[Company]{}, ErrInvalid
	}
	return s.Read.Companies(ctx, limit, offset)
}
func (s Service) Jobs(ctx context.Context, statuses []string, limit, offset int) (Page[Job], error) {
	if !validPage(limit, offset) || len(statuses) == 0 || len(statuses) > 2 {
		return Page[Job]{}, ErrInvalid
	}
	seen := make(map[string]bool, len(statuses))
	for _, status := range statuses {
		if seen[status] {
			return Page[Job]{}, ErrInvalid
		}
		seen[status] = true
		if status != "failed" && status != "retry_pending" {
			return Page[Job]{}, ErrInvalid
		}
	}
	return s.Read.Jobs(ctx, statuses, limit, offset)
}
func (s Service) Activities(ctx context.Context, limit, offset int) (Page[Activity], error) {
	if !validPage(limit, offset) {
		return Page[Activity]{}, ErrInvalid
	}
	return s.Read.Activities(ctx, limit, offset)
}
func validPage(limit, offset int) bool {
	return limit >= 1 && limit <= 100 && offset >= 0 && offset <= 10000
}
func (s Service) Distribution(ctx context.Context) (Distribution, error) {
	return s.Read.Distribution(ctx)
}
func (s Service) Storage(ctx context.Context) (Storage, error) {
	storage, err := s.Read.Storage(ctx)
	if err != nil {
		return storage, err
	}
	// The quota is platform configuration, not measurement: the repository
	// reports what was counted, the service says what it is counted against.
	if s.StorageQuota != nil && *s.StorageQuota > 0 {
		storage.QuotaBytes = s.StorageQuota
		storage.QuotaState = "limited"
	}
	if storage.APIRequests != nil {
		storage.APIRequests.Limit = s.APIRequestQuota
	}
	return storage, nil
}
func (s Service) IntegrationHealth(ctx context.Context) ([]Integration, error) {
	return s.Read.IntegrationHealth(ctx)
}

// Notifications returns the operator's feed. The operator must be the
// authenticated platform user, never a value supplied by the client.
func (s Service) Notifications(ctx context.Context, operator string, filter NotificationFilter, limit int) (NotificationPage, error) {
	types, err := filter.types()
	if err != nil || !filter.validRead() || operator == "" {
		return NotificationPage{}, ErrInvalid
	}
	// The transport always supplies a limit; zero is rejected instead of silently
	// falling back to a default so a bad client value cannot look successful.
	if limit < 1 || limit > 50 {
		return NotificationPage{}, ErrInvalid
	}
	page, err := s.Read.Notifications(ctx, operator, types, filter.Read, limit)
	if err != nil {
		return NotificationPage{}, err
	}
	// Echo the applied filter so a stale response cannot be mistaken for another
	// filter's result.
	page.Type, page.Read = filter.Type, filter.Read
	return page, nil
}

// MarkNotificationsRead stores or clears read markers for the given keys.
// Unknown keys are accepted: a notification can disappear between the read and
// the mark (a retried job becomes delivered), and that must not fail the call.
func (s Service) MarkNotificationsRead(ctx context.Context, operator string, keys []string, read bool) error {
	if operator == "" {
		return ErrInvalid
	}
	unique := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !notificationKeyPattern.MatchString(key) {
			return ErrInvalid
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, key)
	}
	if len(unique) == 0 || len(unique) > MaxNotificationKeys {
		return ErrInvalid
	}
	return s.Read.MarkNotifications(ctx, operator, unique, read)
}

func (s Service) MarkAllNotificationsRead(ctx context.Context, operator string) error {
	if operator == "" {
		return ErrInvalid
	}
	return s.Read.MarkAllNotificationsRead(ctx, operator)
}

// operatorKey carries the authenticated platform user through the request
// context. The transport layer sets it after MFA and capability checks; handlers
// must never read the operator from a client-supplied value.
type operatorKey struct{}

func WithOperator(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, operatorKey{}, userID)
}

func OperatorID(ctx context.Context) string {
	userID, _ := ctx.Value(operatorKey{}).(string)
	return userID
}
