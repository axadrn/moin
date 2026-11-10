package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/service"
)

var contentDir = filepath.Join("..", "..", "content")

// TestRoadmapIsCardsFromTheDocsPage: every heading of docs/roadmap.md is a
// card with its list as a timeline, and the links in the text still work.
func TestRoadmapIsCardsFromTheDocsPage(t *testing.T) {
	recorder := httptest.NewRecorder()
	Roadmap(os.DirFS(contentDir))(recorder, httptest.NewRequest(http.MethodGet, "/roadmap", nil))
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d", recorder.Code)
	}
	for _, want := range []string{
		`class="moin-columns"`, `class="moin-timeline"`,
		">Now</h2>", ">Soon</h2>", ">Later</h2>", ">moin next</h2>",
		`<span class="moin-strong">Deploys</span>`,
		`<a href="/docs/guides/deploy">Deploy</a>`,
		`<a href="https://github.com/axadrn/moin/issues/1">#1</a>`,
		"On every surface",
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
	handler := NewDocsHandler(service.NewDocsService(os.DirFS(contentDir), "", false))
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
	Changelog(os.DirFS(contentDir))(recorder, httptest.NewRequest(http.MethodGet, "/changelog", nil))
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
