package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
)

// Settings lists Registries with the workspace, the workspaces with a check on the open one, Billing only
// for those who may manage it, and Admin only for operators.
func TestSettingsHome(t *testing.T) {
	render := func(role string, admin bool) string {
		t.Helper()
		workspace := &model.Workspace{ID: "workspace-1", Name: "First Workspace", Slug: "first-workspace"}
		member := &model.WorkspaceMember{ID: "member-1", WorkspaceID: workspace.ID, UserID: "user-1", Role: role}
		ctx := context.Background()
		ctx = ctxkeys.WithRenderConfig(ctx, config.RenderConfig{SupportEmail: "support@example.com", Billing: true})
		ctx = ctxkeys.WithURLPath(ctx, "/app/first-workspace/settings")
		ctx = ctxkeys.WithUser(ctx, &model.User{ID: "user-1", Email: "user@example.com", Name: "Test User"})
		ctx = ctxkeys.WithWorkspace(ctx, workspace)
		ctx = ctxkeys.WithWorkspaceMember(ctx, member)
		ctx = ctxkeys.WithWorkspaces(ctx, []model.WorkspaceMembership{{Workspace: *workspace, Role: role}, {Workspace: model.Workspace{ID: "workspace-2", Name: "Second Workspace", Slug: "second-workspace"}, Role: model.WorkspaceRoleMember}})
		ctx = ctxkeys.WithAdmin(ctx, admin)
		var output bytes.Buffer
		if err := SettingsHome(SettingsHomeView{HubBackup: &model.HubBackup{State: model.HubBackupLate, Bucket: "acme-hub"}}).Render(ctx, &output); err != nil {
			t.Fatal(err)
		}
		return output.String()
	}
	owner := render(model.WorkspaceRoleOwner, false)
	for _, want := range []string{"First Workspace", "Second Workspace", `aria-label="Open now"`, `href="/app/second-workspace/settings"`, "/workspaces/new", `href="/app/first-workspace/integrations"`, "Hub backup", "late · bucket acme-hub", `href="/app/first-workspace/registries"`, "/billing", "/settings/account", `hx-post="/auth/logout"`} {
		if !strings.Contains(owner, want) {
			t.Fatalf("settings lack %s", want)
		}
	}
	if strings.Contains(owner, `href="/admin"`) {
		t.Fatal("settings show Admin to a member who is no operator")
	}
	if admin := render(model.WorkspaceRoleAdmin, true); strings.Contains(admin, "/billing") || !strings.Contains(admin, `href="/admin"`) {
		t.Fatal("billing must be owner only and Admin shown to operators")
	}
}

// The hub backup is a card in Settings for the admins of the hub, red when
// it is off or late, green with its last copy when on. Others see nothing.
func TestSettingsHomeHubBackup(t *testing.T) {
	render := func(backup *model.HubBackup) string {
		t.Helper()
		workspace := &model.Workspace{ID: "workspace-1", Name: "First Workspace", Slug: "first-workspace"}
		ctx := context.Background()
		ctx = ctxkeys.WithRenderConfig(ctx, config.RenderConfig{Billing: true})
		ctx = ctxkeys.WithURLPath(ctx, "/app/first-workspace/settings")
		ctx = ctxkeys.WithUser(ctx, &model.User{ID: "user-1", Email: "user@example.com", Name: "Test User"})
		ctx = ctxkeys.WithWorkspace(ctx, workspace)
		var output bytes.Buffer
		if err := SettingsHome(SettingsHomeView{HubBackup: backup}).Render(ctx, &output); err != nil {
			t.Fatal(err)
		}
		return output.String()
	}
	now := time.Now()
	on := render(&model.HubBackup{State: model.HubBackupOn, Bucket: "acme-hub", LastCopy: &now})
	if !strings.Contains(on, `data-hub-backup="on"`) || !strings.Contains(on, "on · bucket acme-hub · last copy just now") {
		t.Errorf("an on hub backup is missing: %s", on)
	}
	if off := render(&model.HubBackup{State: model.HubBackupOff}); !strings.Contains(off, `data-state="failed"`) || !strings.Contains(off, ">off<") {
		t.Errorf("an off hub backup is not red: %s", off)
	}
	if strings.Contains(render(nil), "Hub backup") {
		t.Error("someone who is no admin of the hub sees its backup")
	}
}
