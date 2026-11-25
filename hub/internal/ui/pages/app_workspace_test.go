package pages

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
)

func TestWorkspaceContentUsesViewerPermissionsForMemberActions(t *testing.T) {
	for _, test := range []struct {
		name        string
		viewerRole  string
		memberRole  string
		wantActions bool
	}{
		{name: "owner manages member", viewerRole: model.WorkspaceRoleOwner, memberRole: model.WorkspaceRoleMember, wantActions: true},
		{name: "member cannot manage owner", viewerRole: model.WorkspaceRoleMember, memberRole: model.WorkspaceRoleOwner, wantActions: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = ctxkeys.WithWorkspace(ctx, &model.Workspace{ID: "workspace-1", Name: "Acme", Slug: "acme"})
			ctx = ctxkeys.WithWorkspaceMember(ctx, &model.WorkspaceMember{ID: "viewer", WorkspaceID: "workspace-1", UserID: "viewer", Role: test.viewerRole})
			ctx = ctxkeys.WithSubscription(ctx, &model.Subscription{WorkspaceID: "workspace-1", PlanID: model.SubscriptionPlanPro, Status: model.SubscriptionStatusActive})
			ctx = ctxkeys.WithCSRFToken(ctx, "csrf-token")
			members := []model.WorkspaceMember{{ID: "listed-member", WorkspaceID: "workspace-1", UserID: "listed-member", Name: "Listed Member", Role: test.memberRole}}

			var output bytes.Buffer
			if err := WorkspaceContent(members, nil).Render(ctx, &output); err != nil {
				t.Fatalf("render WorkspaceContent(): %v", err)
			}
			html := output.String()
			for _, action := range []string{
				"/app/acme/workspace/members/listed-member/role",
				"/app/acme/workspace/members/listed-member/remove",
			} {
				if got := strings.Contains(html, action); got != test.wantActions {
					t.Fatalf("action %q present = %v, want %v", action, got, test.wantActions)
				}
			}
		})
	}
}

// The workspace and an app show their ID at the end of their settings, as
// the API answers it, with a copy button.
func TestSettingsShowTheID(t *testing.T) {
	const uuid, id = "0f8e2c4a-1b3d-4e5f-8a9b-0c1d2e3f4a5b", "0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b"
	ctx := context.Background()
	ctx = ctxkeys.WithWorkspace(ctx, &model.Workspace{ID: uuid, Name: "Acme", Slug: "acme"})
	ctx = ctxkeys.WithWorkspaceMember(ctx, &model.WorkspaceMember{ID: "viewer", WorkspaceID: uuid, UserID: "viewer", Role: model.WorkspaceRoleOwner})
	ctx = ctxkeys.WithSubscription(ctx, &model.Subscription{WorkspaceID: uuid, PlanID: model.SubscriptionPlanPro, Status: model.SubscriptionStatusActive})
	ctx = ctxkeys.WithCSRFToken(ctx, "csrf-token")
	for name, page := range map[string]interface {
		Render(context.Context, io.Writer) error
	}{
		"workspace": WorkspaceContent(nil, nil),
		"app":       appSettingsSection(model.App{ID: uuid, Name: "my-app"}, nil, nil),
	} {
		var output bytes.Buffer
		if err := page.Render(ctx, &output); err != nil {
			t.Fatal(err)
		}
		if html := output.String(); !strings.Contains(html, "data-id") || !strings.Contains(html, ">"+id+"</code>") || !strings.Contains(html, `data-copy="`+id+`"`) {
			t.Errorf("%s shows no ID to copy: %s", name, html)
		}
	}
}
