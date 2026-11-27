package service

import (
	"io/fs"
	"slices"
	"strings"

	"github.com/axadrn/moin/hub/internal/markdown"
	"github.com/axadrn/moin/hub/internal/model"
)

// docsGroups are the topics of the docs home, in their order. A page names
// its topic in its front matter (group); a page without one, or with
// another, goes under "More" at the end.
var docsGroups = []string{"Start", "Apps", "Servers and data", "Reference"}

// DocsService reads the pages under docs/: once, or on every request in
// development (live), so an edit shows on reload.
type DocsService struct {
	files  fs.FS
	live   bool
	groups []model.DocGroup
}

func NewDocsService(files fs.FS, live bool) *DocsService {
	return &DocsService{files: files, live: live}
}

// Groups are every page under its topic, in the order of docsGroups, each
// topic's pages by their order. Empty topics are left out.
func (s *DocsService) Groups() ([]model.DocGroup, error) {
	if s.groups != nil && !s.live {
		return s.groups, nil
	}
	parser := markdown.NewParser()
	groups := make([]model.DocGroup, len(docsGroups)+1)
	for i, title := range docsGroups {
		groups[i].Title = title
	}
	groups[len(docsGroups)].Title = "More"
	err := fs.WalkDir(s.files, "docs", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		page, err := readDocPage(parser, s.files, path)
		if err != nil {
			return err
		}
		at := slices.Index(docsGroups, page.Group)
		if at < 0 {
			at = len(docsGroups)
		}
		groups[at].Pages = append(groups[at].Pages, page)
		return nil
	})
	if err != nil {
		return nil, err
	}
	kept := groups[:0]
	for _, group := range groups {
		slices.SortStableFunc(group.Pages, func(a, b *model.DocPage) int { return a.Order - b.Order })
		if len(group.Pages) > 0 {
			kept = append(kept, group)
		}
	}
	s.groups = kept
	return kept, nil
}

// Pages are every page in the order of the docs home, so "Next" walks
// through the docs the way the home lists them.
func (s *DocsService) Pages() ([]*model.DocPage, error) {
	groups, err := s.Groups()
	var pages []*model.DocPage
	for _, group := range groups {
		pages = append(pages, group.Pages...)
	}
	return pages, err
}

func readDocPage(parser *markdown.Parser, files fs.FS, path string) (*model.DocPage, error) {
	source, err := fs.ReadFile(files, path)
	if err != nil {
		return nil, err
	}
	html, meta, err := parser.ParseWithFrontmatter(source)
	if err != nil {
		return nil, err
	}
	text := func(key string) string {
		value, _ := meta[key].(string)
		return value
	}
	// YAML reads a whole number as int, others as float64.
	order, _ := meta["order"].(int)
	if f, ok := meta["order"].(float64); ok {
		order = int(f)
	}
	slug := strings.TrimSuffix(strings.TrimPrefix(path, "docs/"), ".md")
	page := &model.DocPage{
		Title:       text("title"),
		Slug:        slug,
		Group:       text("group"),
		Order:       order,
		Description: text("description"),
		Content:     string(source),
		HTMLContent: string(html),
		Headings:    parser.ExtractHeadings(source),
	}
	if page.Title == "" {
		page.Title = slug
	}
	return page, nil
}
