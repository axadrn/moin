package pages

import (
	"encoding/json"
	"fmt"

	"github.com/axadrn/moin/hub/internal/access"
	"github.com/axadrn/moin/hub/internal/brand"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/components"
	"github.com/axadrn/moin/hub/internal/ui/layouts"
)

// jsonLD is data for search engines, as a script tag. json.Marshal escapes
// < and >, so no value can close the tag.
func jsonLD(data any) string {
	body, _ := json.Marshal(data)
	return `<script type="application/ld+json">` + string(body) + `</script>`
}

// homeJSONLD says what the home page is about: the product, free to host
// yourself and, on a hub with billing, its hosted prices, and who makes it.
func homeJSONLD(appURL string, billing bool) string {
	return jsonLD([]map[string]any{
		{
			"@context":            "https://schema.org",
			"@type":               "SoftwareApplication",
			"name":                brand.Name,
			"description":         brand.Description,
			"applicationCategory": "DeveloperApplication",
			"operatingSystem":     "Linux",
			"url":                 appURL,
			"license":             layouts.GitHubURL + "/blob/main/LICENSE",
			"offers":              homeOffers(billing),
		},
		{
			"@context": "https://schema.org",
			"@type":    "Organization",
			"name":     brand.Name,
			"url":      appURL,
			"logo":     appURL + components.AssetURL("/assets/img/logo.svg"),
			"email":    brand.SupportEmail,
			"sameAs":   []string{layouts.GitHubURL, layouts.XURL},
		},
	})
}

// homeOffers are the prices the home page shows: self-hosted for free and,
// with billing, the hosted plan by the month and by the year.
func homeOffers(billing bool) []map[string]any {
	offers := []map[string]any{offer("Self-hosted", 0)}
	if !billing {
		return offers
	}
	for _, plan := range access.Product.Plans {
		if plan.Purchasable {
			offers = append(offers, offer("Hosted, a month per workspace", plan.MonthlyPrice), offer("Hosted, a year per workspace", plan.YearlyPrice))
		}
	}
	return offers
}

// offer is one price in US cents.
func offer(name string, cents int) map[string]any {
	return map[string]any{"@type": "Offer", "name": name, "price": fmt.Sprintf("%d.%02d", cents/100, cents%100), "priceCurrency": "USD"}
}

// blogJSONLD says what a post is, who wrote it and when.
func blogJSONLD(post service.BlogPost, appURL string) string {
	return jsonLD(map[string]any{
		"@context":         "https://schema.org",
		"@type":            "BlogPosting",
		"headline":         post.Title,
		"description":      post.Description,
		"datePublished":    post.Date.Format("2006-01-02"),
		"url":              appURL + "/blog/" + post.Slug,
		"mainEntityOfPage": appURL + "/blog/" + post.Slug,
		"author":           map[string]any{"@type": "Person", "name": "Axel Adrian", "url": "https://x.com/axadrn"},
		"publisher":        map[string]any{"@type": "Organization", "name": brand.Name, "url": appURL},
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
