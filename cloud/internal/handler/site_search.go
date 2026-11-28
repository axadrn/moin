package handler

import (
	"net/http"
	"strings"

	"github.com/axadrn/moin/cloud/internal/catalog"
	"github.com/axadrn/moin/cloud/internal/service"
	"github.com/axadrn/moin/cloud/internal/ui/pages"
)

// siteHits is how many answers the search of the site shows at most.
const siteHits = 20

// SiteSearchHandler is the one search of the public site: the docs and the
// catalog.
type SiteSearchHandler struct {
	docs    *service.DocsService
	catalog []catalog.Entry
}

func NewSiteSearchHandler(docs *service.DocsService, entries []catalog.Entry) *SiteSearchHandler {
	return &SiteSearchHandler{docs: docs, catalog: entries}
}

// Search answers /search?q=: for the palette only its answers, else a page
// with the search, for a browser without JavaScript.
func (h *SiteSearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	groups, err := h.docs.Groups()
	if err != nil {
		renderError(w, "search unavailable", err)
		return
	}
	hits := pages.FindSite(groups, h.catalog, query, siteHits)
	w.Header().Add("Vary", "HX-Request")
	if isAction(r) {
		render(w, r, pages.SiteSearchOptions(query, hits))
		return
	}
	render(w, r, pages.SearchPage(query, hits))
}
