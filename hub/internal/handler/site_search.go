package handler

import (
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"

	"github.com/axadrn/moin"
)

// siteHits is how many answers the search of the site shows at most.
const siteHits = 20

// SiteSearchHandler is the one search of the public site: the docs, the kit and
// the catalog.
type SiteSearchHandler struct {
	docs    *service.DocsService
	catalog *service.CatalogService
	kit     []pages.SiteHit
}

func NewSiteSearchHandler(docs *service.DocsService, catalog *service.CatalogService) *SiteSearchHandler {
	return &SiteSearchHandler{docs: docs, catalog: catalog, kit: kitHits()}
}

// kitLink is a line of the two lists the kit keeps: - [Name](file.md).
var kitLink = regexp.MustCompile(`(?m)^- \[(.+?)\]\((.+?)\.(?:md|html)\)$`)

// kitHits are the parts and rooms of the kit, for the search of the site.
func kitHits() []pages.SiteHit {
	var hits []pages.SiteHit
	for _, list := range []string{"kit/docs/README.md", "kit/rooms/README.md"} {
		text, err := moin.Kit.ReadFile(list)
		if err != nil {
			slog.Error("read the lists of the kit", "error", err)
			continue
		}
		for _, match := range kitLink.FindAllStringSubmatch(string(text), -1) {
			hits = append(hits, pages.SiteHit{Title: match[1], Where: "Kit", Href: "/kit/showroom/#" + match[2]})
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
	hits := pages.FindSite(h.docs.Groups(), apps, h.kit, query, siteHits)
	w.Header().Add("Vary", "HX-Request")
	if isAction(r) {
		render(w, r, pages.SiteSearchOptions(query, hits))
		return
	}
	render(w, r, pages.SearchPage(query, hits))
}
