package service

import (
	"strings"
	"testing"
	"time"
)

// Each page is its absolute address, with its date where it has one.
func TestSitemap(t *testing.T) {
	out, err := Sitemap("https://example.com", []SitemapPage{{Path: "/"}, {Path: "/changelog", Changed: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<loc>https://example.com/</loc>", "<loc>https://example.com/changelog</loc>", "<lastmod>2026-11-03</lastmod>"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("sitemap misses %s:\n%s", want, out)
		}
	}
}
