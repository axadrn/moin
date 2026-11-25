package layouts

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

// The panel has one quiet bar and no sidebar or menus: the wordmark, the
// search that opens the palette, and your picture, which opens Settings.
func TestAppLayoutHasTheBarAndNoSidebar(t *testing.T) {
	page := renderAppLayout(t, model.WorkspaceRoleOwner, 2)
	for _, hook := range []string{`id="app-bar"`, `id="bar-avatar"`, `href="/app/first-workspace/settings"`, "data-open-palette", `id="palette"`, `id="search"`, `id="search-results"`, "data-app-base=", `id="app-shortcuts"`} {
		if !strings.Contains(page, hook) {
			t.Fatalf("the bar lost %s", hook)
		}
	}
	for _, gone := range []string{`data-slot="sidebar`, "window.templ.sidebar", "app-command", "popovertarget=", "Second Workspace", "Billing"} {
		if strings.Contains(page, gone) {
			t.Fatalf("the panel still has %s", gone)
		}
	}
}

func TestBaseLayoutShowsReadOnlyImpersonationBanner(t *testing.T) {
	operatorID := "operator"
	ctx := context.Background()
	ctx = ctxkeys.WithRenderConfig(ctx, config.RenderConfig{})
	ctx = ctxkeys.WithCSRFToken(ctx, "csrf-token")
	ctx = ctxkeys.WithSession(ctx, &model.Session{ImpersonatorUserID: &operatorID})

	var output bytes.Buffer
	if err := Base().Render(ctx, &output); err != nil {
		t.Fatalf("render Base(): %v", err)
	}
	html := output.String()
	if !strings.Contains(html, "viewing the application as another user") || !strings.Contains(html, "/admin/impersonation/stop") || !strings.Contains(html, "csrf-token") {
		t.Fatal("impersonation banner is incomplete")
	}
}

func renderAppLayout(t *testing.T, role string, workspaceCount int) string {
	t.Helper()
	return renderApp(t, appContext(role, workspaceCount))
}

func appContext(role string, workspaceCount int) context.Context {
	workspace := &model.Workspace{ID: "workspace-1", Name: "First Workspace", Slug: "first-workspace"}
	member := &model.WorkspaceMember{ID: "member-1", WorkspaceID: workspace.ID, UserID: "user-1", Role: role}
	workspaces := []model.WorkspaceMembership{{Workspace: *workspace, Role: member.Role}}
	if workspaceCount > 1 {
		workspaces = append(workspaces, model.WorkspaceMembership{
			Workspace: model.Workspace{ID: "workspace-2", Name: "Second Workspace", Slug: "second-workspace"},
			Role:      model.WorkspaceRoleMember,
		})
	}

	ctx := context.Background()
	ctx = ctxkeys.WithRenderConfig(ctx, config.RenderConfig{SupportEmail: "support@example.com", Billing: true})
	ctx = ctxkeys.WithURLPath(ctx, "/app/first-workspace/overview")
	ctx = ctxkeys.WithUser(ctx, &model.User{ID: "user-1", Email: "user@example.com", Name: "Test User"})
	ctx = ctxkeys.WithWorkspace(ctx, workspace)
	ctx = ctxkeys.WithWorkspaceMember(ctx, member)
	return ctxkeys.WithWorkspaces(ctx, workspaces)
}

func renderApp(t *testing.T, ctx context.Context) string {
	t.Helper()
	var output bytes.Buffer
	if err := App("Overview").Render(ctx, &output); err != nil {
		t.Fatalf("render App(): %v", err)
	}
	return output.String()
}

// Everyone without a passkey is asked for one over every page, until they
// have one. An operator impersonating them is not, and nor is a hub
// without passkeys.
func TestAppAsksForAPasskeyUntilThereIsOne(t *testing.T) {
	withPasskeys := func(ctx context.Context, passkeys int) context.Context {
		ctx = ctxkeys.WithRenderConfig(ctx, config.RenderConfig{Passkeys: true})
		return ctxkeys.WithUser(ctx, &model.User{ID: "user-1", Email: "user@example.com", Name: "Test User", Passkeys: passkeys})
	}
	page := renderApp(t, withPasskeys(appContext(model.WorkspaceRoleOwner, 1), 0))
	if !strings.Contains(page, "Secure your account: add a passkey.") || !strings.Contains(page, `href="/app/first-workspace/settings/account?tab=security"`) {
		t.Fatal("no passkey card without a passkey")
	}
	if page := renderApp(t, withPasskeys(appContext(model.WorkspaceRoleOwner, 1), 1)); strings.Contains(page, "data-passkey-card") {
		t.Fatal("the passkey card stays with a passkey")
	}
	operator := "operator"
	impersonating := ctxkeys.WithSession(withPasskeys(appContext(model.WorkspaceRoleOwner, 1), 0), &model.Session{ImpersonatorUserID: &operator})
	if page := renderApp(t, impersonating); strings.Contains(page, "data-passkey-card") {
		t.Fatal("the passkey card shows while impersonating")
	}
	if page := renderAppLayout(t, model.WorkspaceRoleOwner, 1); strings.Contains(page, "data-passkey-card") {
		t.Fatal("the passkey card shows on a hub without passkeys")
	}
}

// While the trial waits for the first server, its button adds that server,
// for every member. Once the trial runs, it is Subscribe for the owner.
func TestTrialButtonAddsTheFirstServerThenSubscribes(t *testing.T) {
	trial := func(role string, ends *time.Time) string {
		subscription := &model.Subscription{PlanID: model.SubscriptionPlanPro, Status: model.SubscriptionStatusTrialing, CurrentPeriodEnd: ends}
		return renderApp(t, ctxkeys.WithSubscription(appContext(role, 1), subscription))
	}
	for _, role := range []string{model.WorkspaceRoleOwner, model.WorkspaceRoleMember} {
		page := trial(role, nil)
		if !strings.Contains(page, `href="/app/first-workspace/servers"`) || !strings.Contains(page, "Add server") {
			t.Fatalf("a waiting trial has no Add server for the %s", role)
		}
		if strings.Contains(page, "Subscribe") || strings.Contains(page, "An owner can subscribe.") {
			t.Fatalf("a waiting trial still asks the %s to subscribe", role)
		}
	}
	ends := time.Now().Add(10 * 24 * time.Hour)
	page := trial(model.WorkspaceRoleOwner, &ends)
	if !strings.Contains(page, `href="/app/first-workspace/billing"`) || !strings.Contains(page, "Subscribe") || strings.Contains(page, "Add server") {
		t.Fatal("a running trial lost Subscribe")
	}
}

// An admin of a hub without a GitHub App sees one line with the form that
// creates it on GitHub; everyone else sees nothing.
func TestAppOffersTheGitHubAppToItsAdmin(t *testing.T) {
	manifest := &model.GitHubAppManifest{Action: "https://github.com/settings/apps/new?state=s1", Manifest: `{"name":"moin-hub"}`}
	page := renderApp(t, ctxkeys.WithGitHubAppManifest(appContext(model.WorkspaceRoleOwner, 1), manifest))
	for _, want := range []string{"No GitHub App yet.", `method="post"`, `action="https://github.com/settings/apps/new?state=s1"`, `name="manifest"`, `value="{&#34;name&#34;:&#34;moin-hub&#34;}"`, `type="submit"`, "Create GitHub App"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the GitHub App line lacks %s", want)
		}
	}
	card := page[strings.Index(page, "data-github-app-card"):]
	if card = card[:strings.Index(card, "</form>")]; strings.Contains(card, "csrf_token") {
		t.Fatal("the form to GitHub carries the CSRF token")
	}
	if page := renderAppLayout(t, model.WorkspaceRoleOwner, 1); strings.Contains(page, "data-github-app-card") {
		t.Fatal("the GitHub App line shows without a manifest")
	}
}
