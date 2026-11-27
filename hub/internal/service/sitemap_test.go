package service

import (
	"slices"
	"testing"
)

// The sitemap lists the legal pages, the catalog and the blog.
func TestSitemapListsThePublicPages(t *testing.T) {
	sitemap := NewSitemapService(nil, "https://example.com/")
	sitemap.AddCatalog([]string{"umami"})
	var locs []string
	for _, url := range sitemap.staticURLs() {
		locs = append(locs, url.Loc)
	}
	for _, page := range []string{"https://example.com/legal/terms", "https://example.com/catalog/umami", "https://example.com/changelog"} {
		if !slices.Contains(locs, page) {
			t.Errorf("sitemap misses %s: %v", page, locs)
		}
	}
}
