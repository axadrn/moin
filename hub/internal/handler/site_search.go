package handler

import (
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// siteHits is how many answers the search of the site shows at most.
const siteHits = 20

// SiteSearchHandler is the one search of the public site: the docs and
// the catalog.
type SiteSearchHandler struct {
	docs    *service.DocsService
	catalog *service.CatalogService
}

func NewSiteSearchHandler(docs *service.DocsService, catalog *service.CatalogService) *SiteSearchHandler {
	return &SiteSearchHandler{docs: docs, catalog: catalog}
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
	hits := pages.FindSite(h.docs.Groups(), apps, query, siteHits)
	w.Header().Add("Vary", "HX-Request")
	if isAction(r) {
		render(w, r, pages.SiteSearchOptions(query, hits))
		return
	}
	render(w, r, pages.SearchPage(query, hits))
}
