package pages

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/controlplane/internal/model"
)

// The Logs section comes with the logs; only without them (a slow agent)
// it loads them itself.
func TestAppLogsComeWithThePage(t *testing.T) {
	app := model.App{Name: "shop", LiveNumber: 3, LatestNumber: 3}
	render := func(logs *AppLogsView) string {
		var html strings.Builder
		view := AppPageView{App: app, Section: "logs", Logs: logs}
		if err := AppPage(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop/logs"), &html); err != nil {
			t.Fatal(err)
		}
		return html.String()
	}
	page := render(&AppLogsView{Lines: []string{"web-1  | 2026-10-31T10:31:30.123456789Z listening on 8080"}})
	if !strings.Contains(page, "listening on 8080") || strings.Contains(page, "Reading the logs") {
		t.Errorf("the logs do not come with the page: %s", page)
	}
	if page := render(&AppLogsView{Message: "The server is offline."}); !strings.Contains(page, "The server is offline.") {
		t.Error("no reason why the logs are missing")
	}
	if page := render(nil); !strings.Contains(page, `hx-get="/app/tiny-rocket-co/apps/shop/logs/lines"`) || !strings.Contains(page, "Reading the logs") {
		t.Error("without the logs the section does not load them")
	}
}

// The GitHub card renders with the page; only the link form asks GitHub
// for the repos, in a card of the same shape.
func TestAppRepoLoadsOnlyTheRepos(t *testing.T) {
	render := func(view RepoView) string {
		var html strings.Builder
		if err := AppRepo(view).Render(projectPageContext("/app/tiny-rocket-co/apps/shop/deploys"), &html); err != nil {
			t.Fatal(err)
		}
		return html.String()
	}
	app := model.App{Name: "shop"}
	if page := render(RepoView{App: app, Enabled: true}); !strings.Contains(page, "data-connect-github") || !strings.Contains(page, "next=/app/tiny-rocket-co/apps/shop/deploys") || strings.Contains(page, `hx-trigger="load"`) {
		t.Errorf("not connected: no Connect GitHub that comes back: %s", page)
	}
	if page := render(RepoView{App: app, Enabled: true, Connected: true, Loading: true}); !strings.Contains(page, "data-link-repo") || !strings.Contains(page, `hx-trigger="load"`) {
		t.Errorf("loading: no link form that asks for the repos: %s", page)
	}
	app.Repo, app.Branch = "acme/shop", "main"
	if page := render(RepoView{App: app, Enabled: true, Connected: true}); !strings.Contains(page, "data-linked-repo") || !strings.Contains(page, "data-unlink-repo") {
		t.Errorf("linked: no repo or Unlink: %s", page)
	}
}
