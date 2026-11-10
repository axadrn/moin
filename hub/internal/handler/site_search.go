package handler

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// siteHits is how many answers the search of the site shows at most.
const siteHits = 20

// SiteSearchHandler is the one search of the public site: the docs, moin UI
// and the catalog.
type SiteSearchHandler struct {
	docs    *service.DocsService
	uiDocs  *service.DocsService
	uiFiles fs.FS
	catalog *service.CatalogService
}

func NewSiteSearchHandler(docs, uiDocs *service.DocsService, uiFiles fs.FS, catalog *service.CatalogService) *SiteSearchHandler {
	return &SiteSearchHandler{docs: docs, uiDocs: uiDocs, uiFiles: uiFiles, catalog: catalog}
}

// uiHits are the components and blocks of moin UI, for the search of the site.
func (h *SiteSearchHandler) uiHits() []pages.SiteHit {
	var hits []pages.SiteHit
	if _, err := h.uiDocs.DocsTree(); err != nil {
		slog.Error("read the docs of the UI", "error", err)
		return nil
	}
	for _, group := range h.uiDocs.Groups() {
		for _, page := range group.Pages {
			if page.Slug != "start" {
				hits = append(hits, pages.SiteHit{Title: page.Title, Where: "UI", Href: "/ui/" + page.Slug})
			}
		}
	}
	groups, err := service.UIBlocks(h.uiFiles)
	if err != nil {
		slog.Error("read the blocks of the UI", "error", err)
		return hits
	}
	for _, group := range groups {
		for _, block := range group.Blocks {
			hits = append(hits, pages.SiteHit{Title: block.Title, Where: "UI", Href: "/ui/blocks/" + block.Slug})
		}
	}
	return hits
}

// Search answers /search?q=: for the palette (htmx) only its answers, as
// a page the search without JavaScript.
func (h *SiteSearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if _, err := h.docs.DocsTree(); err != nil {
		renderError(w, "search unavailable", err)
		return
	}
	store := h.catalog.Store()
	var apps []pages.CatalogEntryView
	for _, entry := range store.Entries() {
		apps = append(apps, pages.CatalogEntryView{Entry: entry})
	}
	hits := pages.FindSite(h.docs.Groups(), apps, h.uiHits(), query, siteHits)
	w.Header().Add("Vary", "HX-Request")
	if isAction(r) {
		render(w, r, pages.SiteSearchOptions(query, hits))
		return
	}
	render(w, r, pages.SearchPage(query, hits))
}
