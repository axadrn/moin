package handler

import (
	"fmt"
	"html"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/axadrn/moin/cloud/internal/catalog"
	"github.com/axadrn/moin/cloud/internal/ui/pages"
)

// CatalogHandler shows the apps of the catalog, read once at start.
type CatalogHandler struct {
	entries []catalog.Entry
	appURL  string
}

func NewCatalogHandler(entries []catalog.Entry, appURL string) *CatalogHandler {
	return &CatalogHandler{entries: entries, appURL: strings.TrimRight(appURL, "/")}
}

func (h *CatalogHandler) entry(r *http.Request) (catalog.Entry, bool) {
	at := slices.IndexFunc(h.entries, func(e catalog.Entry) bool { return e.Slug == r.PathValue("slug") })
	if at < 0 {
		return catalog.Entry{}, false
	}
	return h.entries[at], true
}

// List is /catalog: every app under its category.
func (h *CatalogHandler) List(w http.ResponseWriter, r *http.Request) {
	render(w, r, pages.Catalog(h.entries))
}

// Show is the page of one app.
func (h *CatalogHandler) Show(w http.ResponseWriter, r *http.Request) {
	entry, ok := h.entry(r)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		render(w, r, pages.NotFound())
		return
	}
	render(w, r, pages.CatalogApp(entry, catalog.Similar(h.entries, entry, 4), h.appURL))
}

// Logo serves an app's logo from the catalog, never from its project's
// site. An SVG runs no script here: the policy forbids it.
func (h *CatalogHandler) Logo(w http.ResponseWriter, r *http.Request) {
	entry, ok := h.entry(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if len(entry.Logo) == 0 {
		// An app without a logo gets its first letter, so every icon shows.
		first, _ := utf8.DecodeRuneInString(entry.Name)
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40"><text x="20" y="27" text-anchor="middle" font-family="system-ui, sans-serif" font-size="22" font-weight="600" fill="#1c1c1e">%s</text></svg>`, html.EscapeString(strings.ToUpper(string(first))))
		return
	}
	w.Header().Set("Content-Type", entry.LogoType)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	_, _ = w.Write(entry.Logo)
}
