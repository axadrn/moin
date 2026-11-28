package service

import (
	"encoding/xml"
	"time"
)

// SitemapPage is one address in sitemap.xml. Only the blog and the
// changelog have a date: search engines ignore one that is always today.
type SitemapPage struct {
	Path    string
	Changed time.Time
}

// Sitemap is sitemap.xml for the pages, each at base plus its path.
func Sitemap(base string, pages []SitemapPage) ([]byte, error) {
	type url struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod,omitempty"`
	}
	type urlset struct {
		XMLName xml.Name `xml:"urlset"`
		XMLNS   string   `xml:"xmlns,attr"`
		URLs    []url    `xml:"url"`
	}
	set := urlset{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, page := range pages {
		entry := url{Loc: base + page.Path}
		if !page.Changed.IsZero() {
			entry.LastMod = page.Changed.Format("2006-01-02")
		}
		set.URLs = append(set.URLs, entry)
	}
	out, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}
