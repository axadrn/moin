package repository_test

import (
	"errors"
	"testing"

	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/testutil"
)

// TestEmailAlertsAreOnByDefault turns the email alerts of a new user off
// and on again.
func TestEmailAlertsAreOnByDefault(t *testing.T) {
	database := testutil.NewDatabase(t)
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "ada", Verified: true})
	users := repository.NewUsers(database)
	members := repository.NewWorkspaceMembers(database)
	for i, on := range []bool{true, false, true} {
		if i > 0 {
			if err := users.SetEmailAlerts("ada", on); err != nil {
				t.Fatal(err)
			}
		}
		user, err := users.ByID("ada")
		if err != nil {
			t.Fatal(err)
		}
		emails, err := members.AlertEmails("workspace-ada")
		if err != nil {
			t.Fatal(err)
		}
		if user.EmailAlerts != on || (len(emails) == 1) != on {
			t.Errorf("on %v: user %v, recipients %v", on, user.EmailAlerts, emails)
		}
	}
	if err := users.SetEmailAlerts("nobody", false); !errors.Is(err, repository.ErrUserNotFound) {
		t.Errorf("unknown user: %v", err)
	}
}
