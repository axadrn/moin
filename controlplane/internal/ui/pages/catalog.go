package pages

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/axadrn/moin/internal/find"

	"github.com/axadrn/moin/controlplane/internal/catalog"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/ui/kit"
	"github.com/axadrn/moin/internal/human"
)

// CatalogEntryView is a catalog app with its releases, newest first.
type CatalogEntryView struct {
	Entry    catalog.Entry
	Versions []catalog.Version
}

// Latest is the newest release; ok is false before the registry answered.
func (v CatalogEntryView) Latest() (catalog.Version, bool) {
	if len(v.Versions) == 0 {
		return catalog.Version{}, false
	}
	return v.Versions[0], true
}

// CatalogView is the catalog page: the apps of the category.
type CatalogView struct {
	Apps       []CatalogEntryView
	Categories []string
	// Category is the picked category; empty shows all.
	Category string
	// Base is where the pages of the apps live: /apps on the site,
	// <workspace>/catalog in the app.
	Base string
	// Sort is SortUpdated or SortName. It and the category live in the
	// URL.
	Sort string
}

// URL is the catalog as it is now, for the address bar.
func (v CatalogView) URL() string {
	return catalogURL(v.Base, v.Category, v.Sort)
}

// categoryOptions are the categories for the select, All first.
func categoryOptions(view CatalogView) []kit.Option {
	options := []kit.Option{{Value: "", Label: "All categories"}}
	for _, category := range view.Categories {
		options = append(options, kit.Option{Value: categorySlug(category), Label: category})
	}
	return options
}

// The orders of the catalog.
const (
	SortUpdated = "updated"
	SortName    = "name"
)

// FilterCatalog keeps the apps of category that query finds (the search of
// the site, FindSite), with find.Name like the palette, so a typo like "nfty" still finds ntfy: on
// their name first, then on their name, category and pitch together. A
// search puts the best answers first; else sort orders them: the newest
// release first, apps without a date last, or by name.
func FilterCatalog(apps []CatalogEntryView, category, query, sort string) []CatalogEntryView {
	type hit struct {
		app   CatalogEntryView
		score int
	}
	var hits []hit
	for _, app := range apps {
		if category != "" && app.Entry.Category != category {
			continue
		}
		score, ok := catalogScore(app.Entry, query)
		if !ok {
			continue
		}
		hits = append(hits, hit{app, score})
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		if a.score != b.score {
			return b.score - a.score
		}
		if sort != SortName {
			if order := releaseTime(b.app).Compare(releaseTime(a.app)); order != 0 {
				return order
			}
		}
		return strings.Compare(strings.ToLower(a.app.Entry.Name), strings.ToLower(b.app.Entry.Name))
	})
	kept := make([]CatalogEntryView, 0, len(hits))
	for _, h := range hits {
		kept = append(kept, h.app)
	}
	return kept
}

// catalogScore is how well query finds an app: a match on its name beats
// one that needs its category or pitch. An empty query finds every app.
func catalogScore(entry catalog.Entry, query string) (int, bool) {
	if strings.TrimSpace(query) == "" {
		return 0, true
	}
	if m, ok := find.Name(query, entry.Name); ok {
		return 1000 + m.Score, true
	}
	text := entry.Name + " " + entry.Category + " " + entry.Pitch
	if len(strings.Fields(query)) > 1 {
		m, ok := find.Name(query, text)
		return m.Score, ok
	}
	// One word is matched against each word on its own: against the whole
	// text find.Name would also take letters strewn over many words
	// ("nfty" in "Nextcloud … files … your").
	best, found := 0, false
	for _, word := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if m, ok := find.Name(query, word); ok && (!found || m.Score > best) {
			best, found = m.Score, true
		}
	}
	return best, found
}

// releaseTime is when the app's newest release came out; zero sorts last.
func releaseTime(app CatalogEntryView) time.Time {
	latest, _ := app.Latest()
	return latest.Date
}

// catalogURL is the catalog with a category, a search and an order; the
// defaults stay out of the URL.
func catalogURL(base, category, sort string) string {
	values := url.Values{}
	if category != "" {
		values.Set("category", categorySlug(category))
	}
	if sort == SortName {
		values.Set("sort", SortName)
	}
	if len(values) == 0 {
		return base
	}
	return base + "?" + values.Encode()
}

// CatalogInstallView is the install form of a catalog app in a workspace.
type CatalogInstallView struct {
	App     CatalogEntryView
	Servers []model.Server
}

// CatalogUpdateView is the update panel of an app from the catalog.
type CatalogUpdateView struct {
	App      model.App
	Entry    catalog.Entry
	Version  string
	Latest   catalog.Version
	Newer    bool
	Versions []catalog.Version
}

// categorySlug is a category in a URL: Developer tools is developer-tools.
func categorySlug(category string) string {
	return strings.ReplaceAll(strings.ToLower(category), " ", "-")
}

// CategoryFromSlug is the category a URL names, or empty.
func CategoryFromSlug(slug string, categories []string) string {
	for _, category := range categories {
		if categorySlug(category) == slug {
			return category
		}
	}
	return ""
}

// released says when a version came out: "released 2d ago", or nothing
// when the registry does not say.
func released(version catalog.Version, now time.Time) string {
	if version.Date.IsZero() {
		return ""
	}
	return "released " + human.Ago(version.Date, now)
}

// catalogJSONLD describes the app for search engines, as JSON-LD.
func catalogJSONLD(view CatalogEntryView, url string) string {
	data := map[string]any{
		"@context":            "https://schema.org",
		"@type":               "SoftwareApplication",
		"name":                view.Entry.Name,
		"description":         view.Entry.Pitch,
		"applicationCategory": view.Entry.Category,
		"operatingSystem":     "Linux, Docker",
		"url":                 url,
		"sameAs":              view.Entry.Website,
		"license":             view.Entry.License,
		"offers":              map[string]any{"@type": "Offer", "price": "0", "priceCurrency": "USD"},
	}
	if latest, ok := view.Latest(); ok {
		data["softwareVersion"] = strings.TrimPrefix(latest.Tag, "v")
	}
	body, _ := json.Marshal(data)
	return `<script type="application/ld+json">` + string(body) + `</script>`
}

// versionOptions are the releases for the version select, newest first.
func versionOptions(versions []catalog.Version) []kit.Option {
	options := make([]kit.Option, 0, len(versions))
	for _, version := range versions {
		options = append(options, kit.Option{Value: version.Tag, Label: version.Tag})
	}
	return options
}

// shownVersions are the newest releases a page lists.
func shownVersions(versions []catalog.Version, limit int) []catalog.Version {
	return versions[:min(len(versions), limit)]
}

// catalogLogo is where a catalog app's icon is: its logo, or its first
// letter when it has none (CatalogHandler.Logo).
func catalogLogo(entry catalog.Entry) string {
	return "/catalog/" + entry.Slug + "/logo"
}

// websiteHost is the host of an app's website, for its row: grafana.com.
func websiteHost(website string) string {
	u, err := url.Parse(website)
	if err != nil || u.Host == "" {
		return website
	}
	return strings.TrimPrefix(u.Host, "www.")
}

// imageURL is the registry page of an image: Docker Hub for a plain name
// (wordpress, grafana/grafana), quay.io's repository page, and the image
// itself on another registry: ghcr.io sends a browser on to its package
// page.
func imageURL(image string) string {
	host, path, found := strings.Cut(image, "/")
	if found && (strings.ContainsAny(host, ".:") || host == "localhost") {
		switch host {
		case "docker.io", "index.docker.io", "registry-1.docker.io":
			image = path
		case "quay.io":
			return "https://quay.io/repository/" + path
		default:
			return "https://" + image
		}
	}
	image = strings.TrimPrefix(image, "library/")
	if !strings.Contains(image, "/") {
		return "https://hub.docker.com/_/" + image
	}
	return "https://hub.docker.com/r/" + image
}
