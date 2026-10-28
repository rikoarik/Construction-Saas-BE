package tenancy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInvitationEmailValidation(t *testing.T) {
	for _, tc := range []struct {
		email string
		valid bool
	}{
		{"person@example.com", true},
		{"First.Last+site@example.co.id", true},
		{"not-an-email", false},
		{"person@", false},
		{"Person <person@example.com>", false},
		{"person@example.com,other@example.com", false},
		{"person@example.com\r\nBcc: other@example.com", false},
	} {
		t.Run(tc.email, func(t *testing.T) {
			if got := validInvitationEmail(tc.email); got != tc.valid {
				t.Errorf("validInvitationEmail(%q) = %v, want %v", tc.email, got, tc.valid)
			}
		})
	}
}

func TestCreateInvitationRejectsInvalidEmailBeforeDatabase(t *testing.T) {
	// A nil pool proves validation runs before opening a transaction.
	store := &Store{}
	_, err := store.CreateInvitation(context.Background(), "actor", "company", "invalid", "member", time.Hour)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}
