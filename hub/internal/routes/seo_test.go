package routes

import (
	"encoding/xml"
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/app"
	"github.com/axadrn/moin/hub/internal/config"
)

var (
	seoTitle       = regexp.MustCompile(`<title[^>]*>([^<]*)</title>`)
	seoDescription = regexp.MustCompile(`<meta name="description" content="([^"]*)"`)
	seoCanonical   = regexp.MustCompile(`<link rel="canonical" href="([^"]*)"`)
	seoRobots      = regexp.MustCompile(`<meta name="robots" content="([^"]*)"`)
	seoH1          = regexp.MustCompile(`<h1[\s>]`)
)

// Every page in the sitemap is ready for search: it answers, it has a title
// and a description of its own, it names itself as canonical and it may be
// indexed. A new page that misses one of these fails here.
func TestSitemapPagesAreReadyForSearch(t *testing.T) {
	const appURL = "https://example.com"
	hub, err := app.New(&config.Config{
		AppEnv:        config.EnvironmentProduction,
		AppURL:        appURL,
		DBConnection:  filepath.Join(t.TempDir(), "app.db"),
		SessionExpiry: time.Hour,
		Billing:       true,
		SecretsKey:    make([]byte, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	site := SetupRoutes(hub)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		site.ServeHTTP(w, httptest.NewRequest(http.MethodGet, appURL+path, nil))
		return w
	}

	if body := get("/robots.txt").Body.String(); !strings.HasPrefix(body, "User-agent: *\n") {
		t.Errorf("robots.txt = %q, want it to start with User-agent", body)
	}

	// One address per page, and the copies and sign-in stay out of search.
	if w := get("/docs/getting-started/?q=1"); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/docs/getting-started?q=1" {
		t.Errorf("/docs/getting-started/ = %d to %q, want 301 to /docs/getting-started?q=1", w.Code, w.Header().Get("Location"))
	}
	if w := get("/ui/button/"); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/ui/button" {
		t.Errorf("/ui/button/ = %d to %q, want 301 to /ui/button", w.Code, w.Header().Get("Location"))
	}
	if w := get("/docs/getting-started.md"); w.Header().Get("X-Robots-Tag") != "noindex" {
		t.Errorf("/docs/getting-started.md has X-Robots-Tag %q, want noindex", w.Header().Get("X-Robots-Tag"))
	}
	if robots := first(seoRobots, get("/auth").Body.String()); robots != "noindex, nofollow" {
		t.Errorf("/auth has robots %q, want noindex, nofollow", robots)
	}

	var sitemap struct {
		URLs []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(get("/sitemap.xml").Body.Bytes(), &sitemap); err != nil {
		t.Fatal(err)
	}
	if len(sitemap.URLs) < 100 {
		t.Fatalf("the sitemap lists %d pages, want every public page", len(sitemap.URLs))
	}

	titles := map[string]string{}
	descriptions := map[string]string{}
	for _, url := range sitemap.URLs {
		path := strings.TrimPrefix(url.Loc, appURL)
		w := get(path)
		if w.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, w.Code)
			continue
		}
		body := w.Body.String()
		title := html.UnescapeString(first(seoTitle, body))
		description := html.UnescapeString(first(seoDescription, body))
		switch {
		case title == "":
			t.Errorf("%s has no title", path)
		case titles[title] != "":
			t.Errorf("%s has the title %q of %s", path, title, titles[title])
		default:
			titles[title] = path
		}
		switch n := len([]rune(description)); {
		case n < 50 || n > 160:
			t.Errorf("%s has a description of %d characters, want 50 to 160: %q", path, n, description)
		case descriptions[description] != "":
			t.Errorf("%s has the description of %s", path, descriptions[description])
		default:
			descriptions[description] = path
		}
		if canonical := first(seoCanonical, body); canonical != url.Loc {
			t.Errorf("%s has the canonical %q, want itself", path, canonical)
		}
		if robots := first(seoRobots, body); robots != "index, follow" {
			t.Errorf("%s has robots %q, want index, follow", path, robots)
		}
		if !seoH1.MatchString(body) {
			t.Errorf("%s has no h1", path)
		}
	}
}

func first(re *regexp.Regexp, body string) string {
	match := re.FindStringSubmatch(body)
	if match == nil {
		return ""
	}
	return strings.TrimSpace(match[1])
}
