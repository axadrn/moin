package pages

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
)

func renderNotice(t *testing.T, info model.VersionInfo) string {
	t.Helper()
	var html strings.Builder
	if err := UpdateNotice(info).Render(projectPageContext("/app/tiny-rocket-co/apps"), &html); err != nil {
		t.Fatal(err)
	}
	return html.String()
}

func TestUpdateNotice(t *testing.T) {
	out := model.VersionInfo{Version: "v0.2.0", SelfHosted: true, Latest: "v0.3.0", UpdateAvailable: true, Summary: "Faster deploys.", NotesURL: "https://github.com/axadrn/moin/releases/tag/v0.3.0", NotesHTML: "<h3>Upgrade</h3><p>Change your tunnel first.</p>", Admin: true}
	html := renderNotice(t, out)
	// The notice shows the command to copy and opens the whole notes.
	for _, want := range []string{"v0.3.0 is out.", "Faster deploys.", "Read what is new, then on the server:", `data-copy="` + model.UpdateCommand + `"`, `data-open-dialog="update-dialog"`, model.ReadFirst, "Change your tunnel first.", "releases/tag/v0.3.0"} {
		if !strings.Contains(html, want) {
			t.Errorf("notice misses %q: %s", want, html)
		}
	}

	// A member, and everyone on the hosted hub, sees nothing.
	member := out
	member.Admin = false
	if html := renderNotice(t, member); strings.Contains(html, "is out") {
		t.Errorf("member sees the notice: %s", html)
	}
	if html := renderNotice(t, model.VersionInfo{Version: "v0.3.0"}); strings.Contains(html, "is out") {
		t.Errorf("hosted shows a notice: %s", html)
	}
}
