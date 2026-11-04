package pages

import (
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/controlplane/internal/catalog"
	"github.com/axadrn/moin/controlplane/internal/model"
	selectcomp "github.com/axadrn/moin/controlplane/internal/ui/components/select"
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

// CatalogView is the catalog page: every app, or those of one category.
type CatalogView struct {
	Apps       []CatalogEntryView
	Categories []string
	// Category is the picked category; empty shows all.
	Category string
	// Base is where the pages of the apps live: /apps on the site,
	// <workspace>/catalog in the app.
	Base string
	// Query is the search, on name and pitch; Sort is SortUpdated or
	// SortName. Both, and the category, live in the URL.
	Query string
	Sort  string
	// Counts are the apps of each category, Total of all of them.
	Counts map[string]int
	Total  int
}

// categoryItems are the categories for the select on small screens, with
// their counts, All first.
func categoryItems(view CatalogView) []selectcomp.ItemData {
	items := []selectcomp.ItemData{{Value: "", Label: "All categories " + strconv.Itoa(view.Total)}}
	for _, category := range view.Categories {
		items = append(items, selectcomp.ItemData{Value: categorySlug(category), Label: category + " " + strconv.Itoa(view.Counts[category])})
	}
	return items
}

// The orders of the catalog.
const (
	SortUpdated = "updated"
	SortName    = "name"
)

// FilterCatalog keeps the apps of category whose name or pitch has query,
// in the order sort asks: the newest release first, apps without a date
// last, or by name.
func FilterCatalog(apps []CatalogEntryView, category, query, sort string) []CatalogEntryView {
	needle := strings.ToLower(strings.TrimSpace(query))
	var kept []CatalogEntryView
	for _, app := range apps {
		if category != "" && app.Entry.Category != category {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(app.Entry.Name+" "+app.Entry.Pitch), needle) {
			continue
		}
		kept = append(kept, app)
	}
	slices.SortStableFunc(kept, func(a, b CatalogEntryView) int {
		if sort != SortName {
			if order := releaseTime(b).Compare(releaseTime(a)); order != 0 {
				return order
			}
		}
		return strings.Compare(strings.ToLower(a.Entry.Name), strings.ToLower(b.Entry.Name))
	})
	return kept
}

// releaseTime is when the app's newest release came out; zero sorts last.
func releaseTime(app CatalogEntryView) time.Time {
	latest, _ := app.Latest()
	return latest.Date
}

// catalogURL is the catalog with a category, a search and an order; the
// defaults stay out of the URL.
func catalogURL(base, category, query, sort string) string {
	values := url.Values{}
	if category != "" {
		values.Set("category", categorySlug(category))
	}
	if strings.TrimSpace(query) != "" {
		values.Set("q", strings.TrimSpace(query))
	}
	if sort == SortName {
		values.Set("sort", SortName)
	}
	if len(values) == 0 {
		return base
	}
	return base + "?" + values.Encode()
}

// releaseUnix is the newest release as a number for the sort in the page.
func releaseUnix(app CatalogEntryView) string {
	at := releaseTime(app)
	if at.IsZero() {
		return "0"
	}
	return strconv.FormatInt(at.Unix(), 10)
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

// monogram is the first letter of an app's name, for its tile.
func monogram(name string) string {
	for _, r := range name {
		return strings.ToUpper(string(r))
	}
	return "?"
}

// released says when a version came out: "released 2d ago", or nothing
// when the registry does not say.
func released(version catalog.Version, now time.Time) string {
	if version.Date.IsZero() {
		return ""
	}
	return "released " + ago(version.Date, now)
}

// releasedShort is when a release came out, always relative: "8 days
// ago", "2 years ago".
func releasedShort(version catalog.Version, now time.Time) string {
	if version.Date.IsZero() {
		return ""
	}
	days := int(now.Sub(version.Date).Hours() / 24)
	switch {
	case days < 1:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 31:
		return strconv.Itoa(days) + " days ago"
	case days < 365:
		if months := days / 30; months > 1 {
			return strconv.Itoa(months) + " months ago"
		}
		return "a month ago"
	default:
		if years := days / 365; years > 1 {
			return strconv.Itoa(years) + " years ago"
		}
		return "a year ago"
	}
}

// databaseSentence says in words what moin does for the app's database.
func databaseSentence(entry catalog.Entry) string {
	if entry.BackedUp() {
		return "moin backs up every change, checked daily"
	}
	if entry.Database == "mysql" {
		return "Runs on MySQL: moin backups for MySQL come next"
	}
	return "Runs on " + entry.DatabaseName() + ": moin doesn't back it up yet"
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

// versionItems are the releases for the version select, newest first.
func versionItems(versions []catalog.Version) []selectcomp.ItemData {
	items := make([]selectcomp.ItemData, 0, len(versions))
	for _, version := range versions {
		items = append(items, selectcomp.ItemData{Value: version.Tag, Label: version.Tag})
	}
	return items
}

// shownVersions are the newest releases a page lists.
func shownVersions(versions []catalog.Version, limit int) []catalog.Version {
	return versions[:min(len(versions), limit)]
}

// catalogSearchText is what the search looks in: name and pitch, lowercase.
func catalogSearchText(app CatalogEntryView) string {
	return strings.ToLower(app.Entry.Name + " " + app.Entry.Pitch)
}

// catalogMatches reports whether the search finds the app.
func catalogMatches(app CatalogEntryView, query string) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	return needle == "" || strings.Contains(catalogSearchText(app), needle)
}
