package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/model"
)

// The alerts page lists the destinations with their hint and state, the
// signing secret once, and the form and the buttons only for owners and
// admins.
func TestAlertsPage(t *testing.T) {
	since := time.Now().Add(-time.Hour)
	created := model.AlertDestination{Name: "ci", Kind: model.AlertWebhook, Hint: "https://ci.example.com/…", Events: "deploy.failed"}
	view := AlertsView{
		Destinations: []model.AlertDestination{
			created,
			{Name: "team", Kind: model.AlertSlack, Hint: "hooks.slack.com/…WXYZ", Events: strings.Join(model.AlertEvents, ","), FailingSince: &since, LastError: "HTTP 404: no_service"},
		},
		Created:       &created,
		SigningSecret: "f00dfeed",
	}
	var html strings.Builder
	if err := Alerts(view).Render(projectPageContext("/app/tiny-rocket-co/alerts"), &html); err != nil {
		t.Fatal(err)
	}
	page := html.String()
	for _, want := range []string{`data-alert-destination="ci"`, `data-alert-destination="team"`, "hooks.slack.com/…WXYZ", "Deploy failed", "every alert", "data-alert-failing", "HTTP 404: no_service", "f00dfeed", "data-alert-form", `hx-post="/app/tiny-rocket-co/alerts/destinations/team/test"`, "Send test"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page has no %q", want)
		}
	}

	member := ctxkeys.WithWorkspaceMember(projectPageContext("/app/tiny-rocket-co/alerts"), &model.WorkspaceMember{WorkspaceID: "workspace-1", UserID: "user-1", Role: model.WorkspaceRoleMember})
	html.Reset()
	if err := Alerts(AlertsView{Destinations: view.Destinations}).Render(member, &html); err != nil {
		t.Fatal(err)
	}
	page = html.String()
	if !strings.Contains(page, `data-alert-destination="team"`) || strings.Contains(page, "data-alert-form") || strings.Contains(page, "Send test") {
		t.Error("a member sees the form or the buttons, or not the list")
	}
}
