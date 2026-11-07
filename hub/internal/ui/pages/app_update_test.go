package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

func renderNotice(t *testing.T, info model.VersionInfo) string {
	t.Helper()
	var html strings.Builder
	if err := UpdateNotice(info).Render(projectPageContext("/app/tiny-rocket-co-workspace1/apps"), &html); err != nil {
		t.Fatal(err)
	}
	return html.String()
}

func TestUpdateNotice(t *testing.T) {
	out := model.VersionInfo{Version: "v0.2.0", SelfHosted: true, Latest: "v0.3.0", UpdateAvailable: true, Summary: "One-click updates.", NotesURL: "https://github.com/axadrn/moin/releases/tag/v0.3.0", Admin: true, Updater: true, CanUpdate: true}
	html := renderNotice(t, out)
	for _, want := range []string{"v0.3.0 is out", "One-click updates.", "Update now", "releases/tag/v0.3.0"} {
		if !strings.Contains(html, want) {
			t.Errorf("notice misses %q: %s", want, html)
		}
	}
	if strings.Contains(html, "every 5s") {
		t.Error("the notice polls without an update under way")
	}

	// Without the updater it says how to update on the server.
	noUpdater := out
	noUpdater.Updater, noUpdater.CanUpdate = false, false
	if html := renderNotice(t, noUpdater); strings.Contains(html, "Update now") || !strings.Contains(html, "sh update.sh") {
		t.Errorf("no updater: %s", html)
	}

	// A member, and everyone on the hosted hub, sees nothing.
	member := out
	member.Admin, member.CanUpdate = false, false
	if html := renderNotice(t, member); strings.Contains(html, "is out") {
		t.Errorf("member sees the notice: %s", html)
	}
	if html := renderNotice(t, model.VersionInfo{Version: "v0.3.0"}); strings.Contains(html, "alert") {
		t.Errorf("hosted shows a notice: %s", html)
	}

	// An update under way polls; a rolled back one says why.
	running := out
	running.Update = &model.UpdateState{State: "updating", Version: "v0.3.0", From: "v0.2.0", At: time.Now()}
	if html := renderNotice(t, running); !strings.Contains(html, "every 5s") || !strings.Contains(html, "Updating moin to v0.3.0") {
		t.Errorf("running: %s", html)
	}
	rolledBack := out
	rolledBack.Update = &model.UpdateState{State: "rolled_back", Version: "v0.3.0", From: "v0.2.0", Reason: "v0.3.0 did not answer within two minutes", At: time.Now()}
	if html := renderNotice(t, rolledBack); !strings.Contains(html, "did not answer") || !strings.Contains(html, "runs v0.2.0 again") {
		t.Errorf("rolled back: %s", html)
	}
}
