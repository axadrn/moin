package service

import (
	"encoding/xml"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
)

var publicRoutes = []struct {
	Path       string
	Priority   string
	ChangeFreq string
}{
	{"/", "1.0", "daily"},
	{"/docs", "0.8", "weekly"},
	{"/ui", "0.8", "weekly"},
	{"/ui/components", "0.7", "weekly"},
	{"/ui/layout", "0.7", "weekly"},
	{"/ui/blocks", "0.7", "weekly"},
	{"/catalog", "0.8", "daily"},
	{"/changelog", "0.6", "weekly"},
	{"/auth", "0.3", "monthly"},
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
}

// AddCatalog lists a page for each catalog app.
func (s *SitemapService) AddCatalog(slugs []string) {
	s.catalog = slugs
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
	today := time.Now().Format("2006-01-02")
	urls := make([]model.SitemapURL, 0, len(publicRoutes))
	for _, route := range publicRoutes {
		urls = append(urls, model.SitemapURL{
			Loc: s.baseURL + route.Path, LastMod: today,
			ChangeFreq: route.ChangeFreq, Priority: route.Priority,
		})
	}
	for _, path := range s.ui {
		urls = append(urls, model.SitemapURL{Loc: s.baseURL + path, LastMod: today, ChangeFreq: "weekly", Priority: "0.6"})
	}
	for _, slug := range s.catalog {
		urls = append(urls, model.SitemapURL{Loc: s.baseURL + "/catalog/" + slug, LastMod: today, ChangeFreq: "daily", Priority: "0.7"})
	}
	for _, slug := range s.compare {
		urls = append(urls, model.SitemapURL{Loc: s.baseURL + "/compare/" + slug, LastMod: today, ChangeFreq: "monthly", Priority: "0.6"})
	}
	if s.legal {
		for _, path := range legalRoutes {
			urls = append(urls, model.SitemapURL{Loc: s.baseURL + path, LastMod: today, ChangeFreq: "yearly", Priority: "0.3"})
		}
	}
	return urls
}
func (s *SitemapService) docsURLs() []model.SitemapURL {
	if err := s.docsService.BuildDocsTree(); err != nil {
		slog.Warn("failed to build docs tree for sitemap", "error", err)
		return nil
	}
	today := time.Now().Format("2006-01-02")
	var urls []model.SitemapURL
	for _, page := range s.docsService.FlatDocsList() {
		priority := "0.6"
		if strings.Count(page.Slug, "/") == 0 {
			priority = "0.8"
		} else if strings.Count(page.Slug, "/") == 1 {
			priority = "0.7"
		}
		urls = append(urls, model.SitemapURL{
			Loc: s.baseURL + "/docs/" + page.Slug, LastMod: today,
			ChangeFreq: "weekly", Priority: priority,
		})
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
