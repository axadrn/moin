package pages

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/axadrn/ops/controlplane/internal/catalog"
	"github.com/axadrn/ops/controlplane/internal/model"
	selectcomp "github.com/axadrn/ops/controlplane/internal/ui/components/select"
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

// databaseNote is what ops does for the app's database.
func databaseNote(database string) string {
	switch database {
	case "postgres":
		return "Postgres backed up"
	case "sqlite":
		return "SQLite backed up"
	}
	return ""
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
