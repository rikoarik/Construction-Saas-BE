package app

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// notificationStub records what the service asked the repository to do, so the
// service's own validation and normalisation can be asserted without a database.
type notificationStub struct {
	Repository
	page      NotificationPage
	err       error
	operator  string
	types     []string
	read      string
	limit     int
	markKeys  []string
	markRead  bool
	markCalls int
	allCalls  int
}

func (s *notificationStub) Notifications(_ context.Context, operator string, types []string, read string, limit int) (NotificationPage, error) {
	s.operator, s.types, s.read, s.limit = operator, types, read, limit
	return s.page, s.err
}

func (s *notificationStub) MarkNotifications(_ context.Context, operator string, keys []string, read bool) error {
	s.markCalls++
	s.operator, s.markKeys, s.markRead = operator, keys, read
	return s.err
}

func (s *notificationStub) MarkAllNotificationsRead(_ context.Context, operator string) error {
	s.allCalls++
	return s.err
}

func TestNotificationFilterValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		filter NotificationFilter
		limit  int
	}{
		{name: "unknown type", filter: NotificationFilter{Type: "jobs"}, limit: 20},
		{name: "unknown read", filter: NotificationFilter{Read: "archived"}, limit: 20},
		{name: "zero limit", limit: 0},
		{name: "negative limit", limit: -1},
		{name: "limit above bound", limit: 51},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := &notificationStub{}
			s := Service{Read: read}
			if _, err := s.Notifications(context.Background(), "operator", tc.filter, tc.limit); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
			// A rejected query must never reach the repository.
			if read.limit != 0 || read.types != nil {
				t.Fatalf("invalid query reached the repository: %+v", read)
			}
		})
	}
	// A missing operator is a bug in the transport, not a client error.
	read := &notificationStub{}
	s := Service{Read: read}
	if _, err := s.Notifications(context.Background(), "", NotificationFilter{}, 20); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty operator: %v", err)
	}
}

func TestNotificationFilterGroupsAndEcho(t *testing.T) {
	for _, tc := range []struct {
		filter NotificationFilter
		types  []string
	}{
		{filter: NotificationFilter{}, types: nil},
		{filter: NotificationFilter{Type: "process"}, types: []string{"process_failed", "process_retry"}},
		{filter: NotificationFilter{Type: "company"}, types: []string{"company_suspended"}},
		{filter: NotificationFilter{Type: "subscription"}, types: []string{"subscription_past_due"}},
	} {
		read := &notificationStub{page: NotificationPage{Items: []Notification{}, Unread: 4}}
		s := Service{Read: read}
		got, err := s.Notifications(context.Background(), "operator", tc.filter, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Items) != 0 || got.Items == nil {
			t.Fatalf("items: %+v", got.Items)
		}
		if len(read.types) != len(tc.types) {
			t.Fatalf("%+v: types %v want %v", tc.filter, read.types, tc.types)
		}
		for i := range tc.types {
			if read.types[i] != tc.types[i] {
				t.Fatalf("%+v: types %v want %v", tc.filter, read.types, tc.types)
			}
		}
		// The response echoes the filter that produced it.
		if got.Type != tc.filter.Type || got.Read != tc.filter.Read {
			t.Fatalf("echo: %+v", got)
		}
		// The unread total is the repository's, unfiltered by the group.
		if got.Unread != 4 {
			t.Fatalf("unread: %d", got.Unread)
		}
		if read.operator != "operator" || read.limit != 20 || read.read != tc.filter.Read {
			t.Fatalf("repository call: %+v", read)
		}
	}
}

func TestNotificationMarkReadValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
	}{
		{name: "no keys", keys: nil},
		{name: "empty list", keys: []string{}},
		{name: "empty key", keys: []string{""}},
		{name: "missing id", keys: []string{"process:"}},
		{name: "unknown prefix", keys: []string{"project:1"}},
		{name: "path traversal", keys: []string{"company:../../etc/passwd"}},
		{name: "whitespace", keys: []string{" process:1"}},
		{name: "sql fragment", keys: []string{"process:1 OR 1=1"}},
		{name: "too many unique keys", keys: uniqueKeys(MaxNotificationKeys + 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := &notificationStub{}
			s := Service{Read: read}
			if err := s.MarkNotificationsRead(context.Background(), "operator", tc.keys, true); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
			if read.markCalls != 0 {
				t.Fatal("invalid keys reached the repository")
			}
		})
	}
}

func TestNotificationMarkReadNormalisesKeys(t *testing.T) {
	read := &notificationStub{}
	s := Service{Read: read}
	// Duplicates collapse, so a client repeating a key cannot exceed the bound.
	keys := append(uniqueKeys(MaxNotificationKeys), uniqueKeys(MaxNotificationKeys)...)
	if err := s.MarkNotificationsRead(context.Background(), "operator", keys, true); err != nil {
		t.Fatal(err)
	}
	if read.markCalls != 1 || len(read.markKeys) != MaxNotificationKeys {
		t.Fatalf("keys: %d calls: %d", len(read.markKeys), read.markCalls)
	}
	if !read.markRead || read.operator != "operator" {
		t.Fatalf("call: %+v", read)
	}
	// Unread uses the same path with read=false.
	read = &notificationStub{}
	s = Service{Read: read}
	if err := s.MarkNotificationsRead(context.Background(), "operator", []string{"company:6f1c2b9e-0a3d-4c8e-9f11-2b7d6a5c4e3f"}, false); err != nil {
		t.Fatal(err)
	}
	if read.markRead {
		t.Fatal("unread must pass read=false")
	}
	if err := s.MarkNotificationsRead(context.Background(), "", []string{"process:1"}, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty operator: %v", err)
	}
}

func TestNotificationServiceErrorsPropagate(t *testing.T) {
	boom := errors.New("boom")
	s := Service{Read: &notificationStub{err: boom}}
	if _, err := s.Notifications(context.Background(), "operator", NotificationFilter{}, 20); !errors.Is(err, boom) {
		t.Fatalf("list error: %v", err)
	}
	if err := s.MarkNotificationsRead(context.Background(), "operator", []string{"process:1"}, true); !errors.Is(err, boom) {
		t.Fatalf("mark error: %v", err)
	}
	if err := s.MarkAllNotificationsRead(context.Background(), "operator"); !errors.Is(err, boom) {
		t.Fatalf("mark all error: %v", err)
	}
	if err := s.MarkAllNotificationsRead(context.Background(), ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty operator: %v", err)
	}
}

func TestNotificationKeysAreBounded(t *testing.T) {
	// The marker key is also a database constraint, so the pattern must reject
	// anything longer than the column's check allows.
	long := "process:" + strings.Repeat("1", 65)
	service := Service{Read: &notificationStub{}}
	if err := service.MarkNotificationsRead(context.Background(), "operator", []string{long}, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("over-long key accepted: %v", err)
	}
	if !notificationKeyPattern.MatchString("process:" + strings.Repeat("1", 64)) {
		t.Fatal("maximum-length key rejected")
	}
}

func uniqueKeys(count int) []string {
	keys := make([]string, 0, count)
	for i := 0; i < count; i++ {
		keys = append(keys, "process:"+itoa(int64(i+1)))
	}
	return keys
}

func itoa(value int64) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func TestOperatorContext(t *testing.T) {
	if OperatorID(context.Background()) != "" {
		t.Fatal("background context must not carry an operator")
	}
	ctx := WithOperator(context.Background(), "user-1")
	if OperatorID(ctx) != "user-1" {
		t.Fatal("operator not stored")
	}
}
