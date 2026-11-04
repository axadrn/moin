package handler

import (
	"fmt"
	"github.com/axadrn/moin/controlplane/internal/catalog"
	"github.com/axadrn/moin/controlplane/internal/service"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type SEOHandler struct {
	sitemapService *service.SitemapService
	docsService    *service.DocsService
	baseURL        string
	llmsPath       string
	robotsNoIndex  bool
}

func NewSEOHandler(
	docsService *service.DocsService,
	baseURL string,
	contentPath string,
	robotsNoIndex bool,
	legal bool,
) *SEOHandler {
	sitemap := service.NewSitemapService(docsService, baseURL, legal)
	if entries, err := catalog.Load(filepath.Join(contentPath, "catalog")); err == nil {
		slugs := make([]string, 0, len(entries))
		for _, entry := range entries {
			slugs = append(slugs, entry.Slug)
		}
		sitemap.AddCatalog(slugs)
	}
	return &SEOHandler{
		sitemapService: sitemap,
		docsService:    docsService,
		baseURL:        strings.TrimSuffix(baseURL, "/"),
		llmsPath:       filepath.Join(contentPath, "llms.txt"),
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
	content, err := os.ReadFile(h.llmsPath)
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
		if page.Slug == "introduction" {
			path = "/docs.md"
		}
		description := strings.TrimSpace(page.Description)
		if description == "" {
			description = "Product documentation"
		}
		_, _ = fmt.Fprintf(w, "- [%s](%s%s): %s\n", page.Title, h.baseURL, path, description)
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
