package service

import (
	"slices"
	"testing"
)

// TestSitemapListsLegalPagesOnlyWhereServed lists terms and privacy on the
// hosted service and never the password sign-in page.
func TestSitemapListsLegalPagesOnlyWhereServed(t *testing.T) {
	for _, legal := range []bool{true, false} {
		locs := []string{}
		for _, url := range NewSitemapService(nil, "https://example.com/", legal).staticURLs() {
			locs = append(locs, url.Loc)
		}
		if slices.Contains(locs, "https://example.com/auth/password") {
			t.Fatalf("sitemap lists /auth/password: %v", locs)
		}
		for _, page := range []string{"https://example.com/legal/terms", "https://example.com/legal/privacy"} {
			if slices.Contains(locs, page) != legal {
				t.Fatalf("legal %v: sitemap %v, page %s", legal, locs, page)
			}
		}
	}
}
