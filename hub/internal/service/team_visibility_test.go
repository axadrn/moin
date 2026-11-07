package service

import (
	"slices"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/testutil"
)

// Only members who may manage members see the pending invitations and the
// IP and user agent of a request, whatever the transport asks; a page of
// activity has 1 to 200 events.
func TestOnlyManagersSeeInvitationsAndRequests(t *testing.T) {
	database, workspaces, _ := workspaceTestService(t)
	owner := testutil.SeedUser(t, database, testutil.UserOptions{ID: "manager-owner", Email: "owner@example.com", Name: "Owner"})
	source := Source{Via: "web", IP: "203.0.113.7", UserAgent: "Firefox"}
	if err := workspaces.Invite(owner.WorkspaceMember, owner.Workspace, "ada@example.com", model.WorkspaceRoleMember, source); err != nil {
		t.Fatal(err)
	}
	member := &model.WorkspaceMember{WorkspaceID: owner.Workspace.ID, UserID: "someone", Role: model.WorkspaceRoleMember}

	if invitations, err := workspaces.PendingInvitations(owner.WorkspaceMember); err != nil || len(invitations) != 1 {
		t.Fatalf("the owner's invitations = %v, %v", invitations, err)
	}
	if invitations, err := workspaces.PendingInvitations(member); err != nil || len(invitations) != 0 {
		t.Fatalf("a member's invitations = %v, %v", invitations, err)
	}

	events, _, err := workspaces.ActivityPage(owner.WorkspaceMember, model.ActivityFilter{}, "", 0)
	if err != nil || len(events) != 1 || events[0].IP != "203.0.113.7" || events[0].UserAgent != "Firefox" {
		t.Fatalf("the owner's activity, a page of one = %+v, %v", events, err)
	}
	events, _, err = workspaces.ActivityPage(member, model.ActivityFilter{}, "", 5000)
	if err != nil || len(events) == 0 {
		t.Fatalf("a member's activity = %+v, %v", events, err)
	}
	for _, event := range events {
		if event.IP != "" || event.UserAgent != "" {
			t.Fatalf("a member sees the request: %+v", event)
		}
	}
}

// The activity names the actor to the whole team; only admins and owners see
// the email, as in Members, also in the filter choices. An account that is
// gone is "A former member" to the rest of the team.
func TestOnlyManagersSeeActorEmails(t *testing.T) {
	database, workspaces, _ := workspaceTestService(t)
	owner := testutil.SeedUser(t, database, testutil.UserOptions{ID: "emails-owner", Email: "owner@example.com", Name: "Owner"})
	if err := workspaces.Invite(owner.WorkspaceMember, owner.Workspace, "ada@example.com", model.WorkspaceRoleMember, Source{Via: "web"}); err != nil {
		t.Fatal(err)
	}
	gone := createWorkspaceTestUser(t, database, "emails-gone", "gone@example.com")
	err := repository.NewActivity(database).Create(&model.ActivityEvent{ID: "gone-event", WorkspaceID: owner.Workspace.ID, ActorUserID: &gone.ID, Action: "app.deployed", Metadata: "{}", CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM users WHERE id = $1`, gone.ID); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		role        string
		gone, email string
		choices     []string
		emails      []string
	}{
		{role: model.WorkspaceRoleAdmin, gone: "gone@example.com", email: "owner@example.com", choices: []string{"gone@example.com", "Owner"}, emails: []string{"gone@example.com", "owner@example.com"}},
		{role: model.WorkspaceRoleMember, gone: "A former member", choices: []string{"Owner"}},
	} {
		t.Run(test.role, func(t *testing.T) {
			viewer := &model.WorkspaceMember{WorkspaceID: owner.Workspace.ID, UserID: "viewer", Role: test.role}
			events, _, err := workspaces.ActivityPage(viewer, model.ActivityFilter{}, "", 50)
			if err != nil {
				t.Fatal(err)
			}
			var mine, theirs *model.ActivityEvent
			for i, event := range events {
				switch {
				case event.ActorUserID == nil:
					theirs = &events[i]
				case *event.ActorUserID == owner.User.ID:
					mine = &events[i]
				}
			}
			if mine == nil || theirs == nil {
				t.Fatalf("events = %+v", events)
			}
			if mine.Actor() != "Owner" || mine.ActorName != "Owner" || mine.ActorEmail != test.email {
				t.Fatalf("the owner's event = %q, name %q, email %q, want Owner, Owner, %q", mine.Actor(), mine.ActorName, mine.ActorEmail, test.email)
			}
			if theirs.Actor() != test.gone || (test.email == "" && theirs.ActorEmail != "") {
				t.Fatalf("the gone account's event = %q, email %q, want %q", theirs.Actor(), theirs.ActorEmail, test.gone)
			}

			choices, err := workspaces.ActivityChoices(viewer)
			if err != nil {
				t.Fatal(err)
			}
			var labels, emails []string
			for _, user := range choices.Users {
				labels = append(labels, user.Label())
				if user.Email != "" {
					emails = append(emails, user.Email)
				}
			}
			if !slices.Equal(labels, test.choices) || !slices.Equal(emails, test.emails) {
				t.Fatalf("filter choices = %v, emails %v, want %v, %v", labels, emails, test.choices, test.emails)
			}
		})
	}
}
