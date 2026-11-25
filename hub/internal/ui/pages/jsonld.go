package pages

import (
	"encoding/json"

	"github.com/axadrn/moin/hub/internal/brand"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/ui/components"
)

// jsonLD is data for search engines, as a script tag. json.Marshal escapes
// < and >, so no value can close the tag.
func jsonLD(data any) string {
	body, _ := json.Marshal(data)
	return `<script type="application/ld+json">` + string(body) + `</script>`
}

// homeJSONLD says what the home page is about: the product, free to host
// yourself, and who makes it.
func homeJSONLD(appURL string) string {
	return jsonLD([]map[string]any{
		{
			"@context":            "https://schema.org",
			"@type":               "SoftwareApplication",
			"name":                brand.Name,
			"description":         brand.Description,
			"applicationCategory": "DeveloperApplication",
			"operatingSystem":     "Linux",
			"url":                 appURL,
			"offers":              map[string]any{"@type": "Offer", "price": "0", "priceCurrency": "USD"},
		},
		{
			"@context": "https://schema.org",
			"@type":    "Organization",
			"name":     brand.Name,
			"url":      appURL,
			"logo":     appURL + components.AssetURL("/assets/img/logo.svg"),
			"email":    brand.SupportEmail,
		},
	})
}

// docsJSONLD is the way from the docs to a page, for the crumbs under a
// search result. A folder such as Guides is no page of its own, so the way
// is the docs, then the page.
func docsJSONLD(page *model.DocPage, appURL string) string {
	return jsonLD(map[string]any{
		"@context": "https://schema.org",
		"@type":    "BreadcrumbList",
		"itemListElement": []map[string]any{
			{"@type": "ListItem", "position": 1, "name": "Docs", "item": appURL + "/docs"},
			{"@type": "ListItem", "position": 2, "name": page.Title, "item": appURL + docsPageURL(page)},
		},
	})
}
