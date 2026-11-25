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

// Has reports whether user is one of them.
func (a HubAdmins) Has(user *model.User) bool {
	if user == nil || user.EmailVerifiedAt == nil {
		return false
	}
	_, ok := a[strings.ToLower(user.Email)]
	return ok
}
