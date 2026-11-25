package service

import (
	"strings"

	"github.com/axadrn/moin/hub/internal/model"
)

// HubAdmins are the people who run this hub, hosted or self-hosted: the
// addresses of ADMIN_EMAILS, verified.
type HubAdmins map[string]struct{}

func NewHubAdmins(emails []string) HubAdmins {
	admins := make(HubAdmins, len(emails))
	for _, address := range emails {
		admins[strings.ToLower(strings.TrimSpace(address))] = struct{}{}
	}
	return admins
}

// Has reports whether user is one of them, with the address verified.
func (a HubAdmins) Has(user *model.User) bool {
	return user != nil && user.EmailVerifiedAt != nil && a.Listed(user.Email)
}

// Listed reports whether address is in ADMIN_EMAILS, verified or not.
func (a HubAdmins) Listed(address string) bool {
	_, ok := a[strings.ToLower(strings.TrimSpace(address))]
	return ok
}
