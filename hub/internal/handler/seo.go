package handler

import (
	"fmt"
	"github.com/axadrn/moin/hub/internal/brand"
	"github.com/axadrn/moin/hub/internal/catalog"
	"github.com/axadrn/moin/hub/internal/compare"
	"github.com/axadrn/moin/hub/internal/service"
	"io/fs"
	"net/http"
	"strings"
)

type SEOHandler struct {
	sitemapService *service.SitemapService
	docsService    *service.DocsService
	catalog        []catalog.Entry
	baseURL        string
	files          fs.FS
	robotsNoIndex  bool
}

func NewSEOHandler(docsService *service.DocsService, entries []catalog.Entry, baseURL string, files fs.FS, robotsNoIndex bool) *SEOHandler {
	sitemap := service.NewSitemapService(docsService, baseURL)
	slugs := make([]string, 0, len(entries))
	for _, entry := range entries {
		slugs = append(slugs, entry.Slug)
	}
	sitemap.AddCatalog(slugs)
	sitemap.AddCompare(compare.Slugs())
	if posts, err := service.BlogPosts(files); err == nil {
		sitemap.AddBlog(posts)
	}
	if releases, err := service.ChangelogReleases(files); err == nil && len(releases) > 0 {
		sitemap.AddChangelog(releases[0].Date)
	}
	return &SEOHandler{
		sitemapService: sitemap,
		docsService:    docsService,
		catalog:        entries,
		baseURL:        strings.TrimSuffix(baseURL, "/"),
		files:          files,
		robotsNoIndex:  robotsNoIndex,
	}
}

func (h *SEOHandler) Robots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if h.robotsNoIndex {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
		return
	}
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nSitemap: %s/sitemap.xml\n", h.baseURL)
}

func (h *SEOHandler) LLMs(w http.ResponseWriter, r *http.Request) {
	content, err := fs.ReadFile(h.files, "llms.txt")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := h.docsService.DocsTree(); err != nil {
		http.Error(w, "Failed to load documentation", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprint(w, strings.TrimRight(string(content), "\n"))
	_, _ = fmt.Fprint(w, "\n\n## Documentation\n\n")
	for _, page := range h.docsService.FlatDocsList() {
		path := "/docs/" + page.Slug + ".md"
		description := strings.TrimSpace(page.Description)
		if description == "" {
			description = "Product documentation"
		}
		_, _ = fmt.Fprintf(w, "- [%s](%s%s): %s\n", page.Title, h.baseURL, path, description)
	}
	if posts, err := service.BlogPosts(h.files); err == nil && len(posts) > 0 {
		_, _ = fmt.Fprint(w, "\n## Blog\n\n")
		for _, post := range posts {
			_, _ = fmt.Fprintf(w, "- [%s](%s/blog/%s): %s\n", post.Title, h.baseURL, post.Slug, post.Description)
		}
	}
	if len(h.catalog) > 0 {
		_, _ = fmt.Fprint(w, "\n## Catalog\n\n")
		for _, entry := range h.catalog {
			_, _ = fmt.Fprintf(w, "- [%s](%s/catalog/%s): %s\n", entry.Name, h.baseURL, entry.Slug, entry.Pitch)
		}
	}
	if len(compare.Competitors) > 0 {
		_, _ = fmt.Fprint(w, "\n## Compare\n\n")
		for _, competitor := range compare.Competitors {
			_, _ = fmt.Fprintf(w, "- [%s vs %s](%s/compare/%s): %s\n", brand.Name, competitor.Name, h.baseURL, competitor.Slug, competitor.Lead)
		}
	}
}

func (h *SEOHandler) Sitemap(w http.ResponseWriter, _ *http.Request) {
	content, err := h.sitemapService.GenerateSitemap()
	if err != nil {
		http.Error(w, "Failed to generate sitemap", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(content)
}
