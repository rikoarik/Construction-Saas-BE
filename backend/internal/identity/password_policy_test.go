package identity

import (
	"strings"
	"testing"
)

func TestValidNewPassword(t *testing.T) {
	for _, tc := range []struct {
		password string
		valid    bool
	}{
		{"1234567", false},
		{"12345678", true},
		{"éééééééé", true},
		{"ééééééé", false},
		{strings.Repeat("x", 1024), true},
		{strings.Repeat("x", 1025), false},
		{"1234567\xff", false},
	} {
		if got := ValidNewPassword(tc.password); got != tc.valid {
			t.Errorf("ValidNewPassword(%q) = %v, want %v", tc.password, got, tc.valid)
		}
	}
}
