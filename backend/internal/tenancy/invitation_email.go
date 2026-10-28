package tenancy

import (
	"net/mail"
	"strings"
)

// validInvitationEmail accepts a single mailbox, not a display name or list.
func validInvitationEmail(email string) bool {
	if strings.ContainsAny(email, "\r\n") {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Name == "" && address.Address == email
}
