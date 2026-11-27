package handler

import (
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/axadrn/moin/cloud/internal/brand"
	"github.com/axadrn/moin/cloud/internal/catalog"
	"github.com/axadrn/moin/cloud/internal/compare"
	"github.com/axadrn/moin/cloud/internal/service"
)

// SEOHandler answers what search engines and AI tools read: robots.txt,
// sitemap.xml and llms.txt.
type SEOHandler struct {
	docs    *service.DocsService
	catalog []catalog.Entry
	baseURL string
	files   fs.FS
}

func NewSEOHandler(docs *service.DocsService, entries []catalog.Entry, baseURL string, files fs.FS) *SEOHandler {
	return &SEOHandler{docs: docs, catalog: entries, baseURL: strings.TrimSuffix(baseURL, "/"), files: files}
}

func (h *SEOHandler) Robots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nSitemap: %s/sitemap.xml\n", h.baseURL)
}

// Sitemap lists every page of the site.
func (h *SEOHandler) Sitemap(w http.ResponseWriter, _ *http.Request) {
	docs, err := h.docs.Pages()
	if err != nil {
		renderError(w, "sitemap: docs", err)
		return
	}
	pages := []service.SitemapPage{{Path: "/"}, {Path: "/why"}, {Path: "/docs"}, {Path: "/catalog"}, {Path: "/compare"}, {Path: "/roadmap"}, {Path: "/legal/terms"}, {Path: "/legal/privacy"}}
	for _, page := range docs {
		// The roadmap has its own page, /roadmap.
		if page.Slug != "roadmap" {
			pages = append(pages, service.SitemapPage{Path: "/docs/" + page.Slug})
		}
	}
	for _, entry := range h.catalog {
		pages = append(pages, service.SitemapPage{Path: "/catalog/" + entry.Slug})
	}
	for _, competitor := range compare.Competitors {
		pages = append(pages, service.SitemapPage{Path: "/compare/" + competitor.Slug})
	}
	if posts, err := service.BlogPosts(h.files); err == nil && len(posts) > 0 {
		pages = append(pages, service.SitemapPage{Path: "/blog", Changed: posts[0].Date})
		for _, post := range posts {
			pages = append(pages, service.SitemapPage{Path: "/blog/" + post.Slug, Changed: post.Date})
		}
	}
	changelog := service.SitemapPage{Path: "/changelog"}
	if releases, err := service.ChangelogReleases(h.files); err == nil && len(releases) > 0 {
		changelog.Changed = releases[0].Date
	}
	content, err := service.Sitemap(h.baseURL, append(pages, changelog))
	if err != nil {
		renderError(w, "sitemap", err)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(content)
}

// LLMs is llms.txt: its text from content/, then every docs page, post,
// catalog app and comparison as a link.
func (h *SEOHandler) LLMs(w http.ResponseWriter, r *http.Request) {
	content, err := fs.ReadFile(h.files, "llms.txt")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	docs, err := h.docs.Pages()
	if err != nil {
		renderError(w, "llms.txt: docs", err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, strings.TrimRight(string(content), "\n"))
	fmt.Fprint(w, "\n\n## Documentation\n\n")
	for _, page := range docs {
		fmt.Fprintf(w, "- [%s](%s/docs/%s.md): %s\n", page.Title, h.baseURL, page.Slug, page.Description)
	}
	if posts, err := service.BlogPosts(h.files); err == nil && len(posts) > 0 {
		fmt.Fprint(w, "\n## Blog\n\n")
		for _, post := range posts {
			fmt.Fprintf(w, "- [%s](%s/blog/%s): %s\n", post.Title, h.baseURL, post.Slug, post.Description)
		}
	}
	fmt.Fprint(w, "\n## Catalog\n\n")
	for _, entry := range h.catalog {
		fmt.Fprintf(w, "- [%s](%s/catalog/%s): %s\n", entry.Name, h.baseURL, entry.Slug, entry.Pitch)
	}
	fmt.Fprint(w, "\n## Compare\n\n")
	for _, competitor := range compare.Competitors {
		fmt.Fprintf(w, "- [%s vs %s](%s/compare/%s): %s\n", brand.Name, competitor.Name, h.baseURL, competitor.Slug, competitor.Lead)
	}
}
