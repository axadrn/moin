package handler

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/axadrn/moin/cloud/internal/service"
)

// roadmapContent is a docs/roadmap.md as the site reads it.
func roadmapContent(t *testing.T) fs.FS {
	t.Helper()
	return fstest.MapFS{"docs/roadmap.md": {Data: []byte("---\ntitle: Roadmap\ndescription: What comes next\n---\n\nWhat moin does.\n\n## Now\n\n- **Deploys**: with [Deploy](/docs/deploy) and `moin status`.\n\n## Later\n\n- **Watcher**: see [#1](https://github.com/axadrn/moin/issues/1).\n")}}
}

// TestRoadmapIsCardsFromTheDocsPage: every heading of docs/roadmap.md is a
// card with its list as a timeline, and the links in the text still work.
func TestRoadmapIsCardsFromTheDocsPage(t *testing.T) {
	recorder := httptest.NewRecorder()
	Roadmap(roadmapContent(t))(recorder, httptest.NewRequest(http.MethodGet, "/roadmap", nil))
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d", recorder.Code)
	}
	for _, want := range []string{
		`class="moin-timeline"`, ">Now</h2>", ">Later</h2>",
		`<span class="moin-strong">Deploys</span>`,
		`<a href="/docs/deploy">Deploy</a>`,
		`<a href="https://github.com/axadrn/moin/issues/1">#1</a>`,
		"<code>moin status</code>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the roadmap misses %q", want)
		}
	}
	if strings.Contains(body, "**") {
		t.Error("markdown is left on the page")
	}
}

func TestDocsRoadmapMovedButItsMarkdownStays(t *testing.T) {
	handler := service.NewDocsService(roadmapContent(t), false)
	if response := serveDocs(t, handler, "/docs/roadmap"); response.Code != http.StatusMovedPermanently || response.Header().Get("Location") != "/roadmap" {
		t.Errorf("/docs/roadmap: %d to %q", response.Code, response.Header().Get("Location"))
	}
	if response := serveDocs(t, handler, "/docs/roadmap.md"); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "## Now") {
		t.Errorf("/docs/roadmap.md: %d", response.Code)
	}
}

// TestChangelogIsATimeline: one entry per version with its anchor, the
// newest open and the others closed, and the feed's links still land.
func TestChangelogIsATimeline(t *testing.T) {
	recorder := httptest.NewRecorder()
	Changelog(os.DirFS(filepath.Join("..", "..", "content")))(recorder, httptest.NewRequest(http.MethodGet, "/changelog", nil))
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d", recorder.Code)
	}
	if !strings.Contains(body, `class="moin-timeline"`) || !strings.Contains(body, `href="/changelog.xml"`) {
		t.Error("no timeline or no RSS button")
	}
	if !strings.Contains(body, `<li id="v100-beta1">`) {
		t.Error("the anchor of a version is gone")
	}
	if strings.Count(body, "<details") < 2 || strings.Count(body, `<details class="moin-expand" data-quiet open`) != 1 {
		t.Error("the newest version starts open, the others closed")
	}
}
