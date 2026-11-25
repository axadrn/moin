package service

import (
	"encoding/xml"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

// publicRoutes are the pages that are not docs, catalog apps or
// comparisons. Sign-in is not one: it carries noindex.
var publicRoutes = []string{
	"/",
	"/docs",
	"/next",
	"/ui",
	"/ui/components",
	"/ui/layout",
	"/ui/blocks",
	"/catalog",
	"/roadmap",
}

// legalRoutes are the terms and privacy pages of the hosted service.
var legalRoutes = []string{"/legal/terms", "/legal/privacy"}

type SitemapService struct {
	docsService *DocsService
	baseURL     string
	// legal lists the terms and privacy pages, which only the hosted
	// service serves.
	legal bool
	// catalog are the slugs of the catalog apps, each a page under /catalog.
	catalog []string
	// compare are the slugs of the tools with a page under /compare.
	compare []string
	// ui are the pages of moin UI.
	ui []string
	// blog are the posts, each with its date.
	blog []BlogPost
	// changelog is the date of the newest release. With the blog posts it
	// is the only real date: search engines ignore a lastmod that is always
	// today, so no other page has one.
	changelog time.Time
}

// AddCatalog lists a page for each catalog app.
func (s *SitemapService) AddCatalog(slugs []string) {
	s.catalog = slugs
}

// AddBlog lists the blog and each post with its date.
func (s *SitemapService) AddBlog(posts []BlogPost) {
	s.blog = posts
}

// AddChangelog lists the changelog with the date of its newest release.
func (s *SitemapService) AddChangelog(newest time.Time) {
	s.changelog = newest
}

// AddCompare lists a page for each compared tool.
func (s *SitemapService) AddCompare(slugs []string) {
	s.compare = slugs
}

func NewSitemapService(
	docsService *DocsService,
	baseURL string,
	legal bool,
) *SitemapService {
	return &SitemapService{
		docsService: docsService,
		baseURL:     strings.TrimSuffix(baseURL, "/"),
		legal:       legal,
	}
}

func (s *SitemapService) GenerateSitemap() ([]byte, error) {
	sitemap := model.Sitemap{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	sitemap.URLs = append(sitemap.URLs, s.staticURLs()...)
	sitemap.URLs = append(sitemap.URLs, s.docsURLs()...)
	output, err := xml.MarshalIndent(sitemap, "", "  ")
	if err != nil {
		return nil, err
	}
	return []byte(xml.Header + string(output)), nil
}

func (s *SitemapService) staticURLs() []model.SitemapURL {
	var paths []string
	paths = append(paths, publicRoutes...)
	paths = append(paths, s.ui...)
	for _, slug := range s.catalog {
		paths = append(paths, "/catalog/"+slug)
	}
	if len(s.compare) > 0 {
		paths = append(paths, "/compare")
	}
	for _, slug := range s.compare {
		paths = append(paths, "/compare/"+slug)
	}
	if s.legal {
		paths = append(paths, legalRoutes...)
	}
	urls := make([]model.SitemapURL, 0, len(paths)+len(s.blog)+2)
	for _, path := range paths {
		urls = append(urls, model.SitemapURL{Loc: s.baseURL + path})
	}
	if len(s.blog) > 0 {
		urls = append(urls, model.SitemapURL{Loc: s.baseURL + "/blog", LastMod: s.blog[0].Date.Format("2006-01-02")})
		for _, post := range s.blog {
			urls = append(urls, model.SitemapURL{Loc: s.baseURL + "/blog/" + post.Slug, LastMod: post.Date.Format("2006-01-02")})
		}
	}
	changelog := model.SitemapURL{Loc: s.baseURL + "/changelog"}
	if !s.changelog.IsZero() {
		changelog.LastMod = s.changelog.Format("2006-01-02")
	}
	return append(urls, changelog)
}

func (s *SitemapService) docsURLs() []model.SitemapURL {
	if err := s.docsService.BuildDocsTree(); err != nil {
		slog.Warn("failed to build docs tree for sitemap", "error", err)
		return nil
	}
	var urls []model.SitemapURL
	for _, page := range s.docsService.FlatDocsList() {
		// The roadmap's own page is /roadmap, in publicRoutes.
		if page.Slug == "roadmap" {
			continue
		}
		urls = append(urls, model.SitemapURL{Loc: s.baseURL + "/docs/" + page.Slug})
	}
	return urls
}

// AddUI adds the pages of moin UI: its docs and its blocks.
func (s *SitemapService) AddUI(docs *DocsService, files fs.FS) {
	if docs == nil {
		return
	}
	if _, err := docs.DocsTree(); err != nil {
		return
	}
	for _, page := range docs.FlatDocsList() {
		if page.Slug != "start" {
			s.ui = append(s.ui, "/ui/"+page.Slug)
		}
	}
	groups, err := UIBlocks(files)
	if err != nil {
		return
	}
	for _, group := range groups {
		for _, block := range group.Blocks {
			s.ui = append(s.ui, "/ui/blocks/"+block.Slug)
		}
	}
}
