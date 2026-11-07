package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/axadrn/moin/controlplane/internal/config"
	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
)

func TestDocsHomeListsEveryPageUnderItsTopic(t *testing.T) {
	response := serveDocs(t, newDocsTestHandler(t), "/docs")
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(strings.ToLower(body), "<!doctype html>") {
		t.Fatalf("status = %d, body is no page", response.Code)
	}
	for _, want := range []string{">Start</h2>", ">Apps</h2>", `href="/docs/getting-started"`, `href="/docs/guides/domains"`, `id="docs-q"`, `hx-get="/docs"`} {
		if !strings.Contains(body, want) {
			t.Errorf("docs home misses %q", want)
		}
	}
	// Start comes before Apps, whatever the folders say.
	if strings.Index(body, ">Start</h2>") > strings.Index(body, ">Apps</h2>") {
		t.Error("topics are out of order")
	}
	if regexp.MustCompile(`<a [^>]*hx-`).MatchString(body) || strings.Contains(body, "hx-boost") {
		t.Error("docs links load whole pages, never with htmx")
	}
}

func TestDocsSearchFindsPagesAndSectionsWithATypo(t *testing.T) {
	handler := newDocsTestHandler(t)
	// Opened as a page, the search renders the home with what it finds.
	body := serveDocs(t, handler, "/docs?q=domians").Body.String()
	if !strings.Contains(body, `href="/docs/guides/domains"`) || strings.Contains(body, ">Start</h2>") {
		t.Errorf("search page does not list only what it finds:\n%s", body)
	}
	if !strings.Contains(body, `value="domians"`) {
		t.Error("search page loses what was typed")
	}
	// A keystroke gets only the list, and the address bar follows.
	request := httptest.NewRequest(http.MethodGet, "/docs?q=first", nil)
	request.Header.Set("HX-Request", "true")
	recorder := httptest.NewRecorder()
	handler.ShowDocs(recorder, request)
	fragment := recorder.Body.String()
	if strings.Contains(strings.ToLower(fragment), "<!doctype html>") || !strings.Contains(fragment, `href="/docs/getting-started#first-step"`) {
		t.Errorf("search answer = %q", fragment)
	}
	if got := recorder.Header().Get("HX-Replace-Url"); got != "/docs?q=first" {
		t.Errorf("HX-Replace-Url = %q", got)
	}
}

func TestDocsPageEndsWithTheNextPage(t *testing.T) {
	body := serveDocs(t, newDocsTestHandler(t), "/docs/getting-started").Body.String()
	for _, want := range []string{"First step", `href="/docs/guides/domains"`, "Next: </span>Domains", `href="/docs"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page misses %q", want)
		}
	}
}

func TestDocsRedirectsWhatIsNoPage(t *testing.T) {
	handler := newDocsTestHandler(t)
	for path, target := range map[string]string{
		"/docs/introduction":    "/docs/getting-started",
		"/docs/introduction.md": "/docs/getting-started.md",
		"/docs/guides":          "/docs",
	} {
		response := serveDocs(t, handler, path)
		if location := response.Header().Get("Location"); location != target {
			t.Errorf("%s goes to %q, want %q", path, location, target)
		}
	}
	if code := serveDocs(t, handler, "/docs/missing").Code; code != http.StatusNotFound {
		t.Errorf("missing page answers %d", code)
	}
}

func TestDocsServesMarkdownSource(t *testing.T) {
	handler := newDocsTestHandler(t)
	page := serveDocs(t, handler, "/docs/getting-started.md")
	if page.Code != http.StatusOK || page.Header().Get("Content-Type") != "text/markdown; charset=utf-8" {
		t.Fatalf("status = %d, Content-Type = %q", page.Code, page.Header().Get("Content-Type"))
	}
	if !strings.Contains(page.Body.String(), "## First step") {
		t.Fatalf("markdown response = %q", page.Body.String())
	}
	index := serveDocs(t, handler, "/docs.md").Body.String()
	if !strings.Contains(index, "## Start") || !strings.Contains(index, "- [Domains](/docs/guides/domains.md): Your own domains") {
		t.Fatalf("index = %q", index)
	}
}

func newDocsTestHandler(t *testing.T) *DocsHandler {
	t.Helper()
	contentPath := t.TempDir()
	docsPath := filepath.Join(contentPath, "docs")
	if err := os.MkdirAll(filepath.Join(docsPath, "guides"), 0o755); err != nil {
		t.Fatalf("create docs directory: %v", err)
	}
	pages := map[string]string{
		"getting-started.md": "---\ntitle: Get started\ndescription: Start here\ngroup: Start\norder: 1\n---\n\n## First step\n\nHello.\n",
		"guides/domains.md":  "---\ntitle: Domains\ndescription: Your own domains\ngroup: Apps\norder: 1\n---\n\nDomains.\n",
	}
	for name, page := range pages {
		if err := os.WriteFile(filepath.Join(docsPath, name), []byte(page), 0o644); err != nil {
			t.Fatalf("write docs page: %v", err)
		}
	}
	return NewDocsHandler(service.NewDocsService(contentPath, ""))
}

func serveDocs(t *testing.T, handler *DocsHandler, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	ctx := ctxkeys.WithRenderConfig(request.Context(), config.RenderConfig{
		AppURL:       "https://example.com",
		SupportEmail: "support@example.com",
	})
	ctx = ctxkeys.WithURLPath(ctx, request.URL.Path)
	ctx = ctxkeys.WithCSRFToken(ctx, "csrf-token")
	request = request.WithContext(ctx)
	recorder := httptest.NewRecorder()

	handler.ShowDocs(recorder, request)
	return recorder
}
