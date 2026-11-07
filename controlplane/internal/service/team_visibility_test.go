package service

import (
	"testing"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/testutil"
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
