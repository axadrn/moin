package service

import (
	"bytes"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/axadrn/moin/hub/internal/markdown"
	"github.com/axadrn/moin/hub/internal/model"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type DocsService struct {
	parser   *markdown.Parser
	files    fs.FS
	docsTree *model.DocPage
	// appURL is this hub, which the docs show in place of the
	// placeholder.
	appURL string
	// groups are the topics, in their order.
	groups []string
	// live reads the pages again on every request, in development.
	live bool
	// examples are the files of the example blocks. Only moin UI has them.
	examples fs.FS
}

// docsPlaceholder is the hub in the doc sources. Every page but
// the self-hosting guide shows the real APP_URL instead, so commands copy
// as they are.
const docsPlaceholder = "https://moin.example.com"

// NewDocsService serves the pages under docs/ in files.
func NewDocsService(files fs.FS, appURL string, live bool) *DocsService {
	return &DocsService{
		parser: markdown.NewParser(markdown.Examples(files)),
		files:  files,
		appURL: strings.TrimRight(appURL, "/"),
		groups: docsGroups,
		live:   live,
	}
}

// NewUIDocsService serves the docs of moin UI, the same way, with its
// examples.
func NewUIDocsService(files fs.FS, appURL string, live bool) *DocsService {
	docs := NewDocsService(files, appURL, live)
	docs.groups = uiGroups
	docs.examples = files
	return docs
}

// withAppURL puts this hub in place of the placeholder. The
// self-hosting guide keeps it: there it stands for the reader's own.
func (s *DocsService) withAppURL(content []byte, relPath string) []byte {
	if s.appURL == "" || relPath == "self-hosting.md" {
		return content
	}
	host := strings.TrimPrefix(strings.TrimPrefix(s.appURL, "https://"), "http://")
	content = bytes.ReplaceAll(content, []byte(docsPlaceholder), []byte(s.appURL))
	return bytes.ReplaceAll(content, []byte(strings.TrimPrefix(docsPlaceholder, "https://")), []byte(host))
}

func (s *DocsService) BuildDocsTree() error {
	tree := &model.DocPage{
		Title:    "Documentation",
		Slug:     "",
		Path:     "",
		Children: []*model.DocPage{},
	}

	// Track directory metadata from _index.md files
	dirMetadata := make(map[string]*model.DocPage)

	err := fs.WalkDir(s.files, "docs", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}

		relPath := strings.TrimPrefix(path, "docs/")

		page, err := s.loadDocPage(path, relPath)
		if err != nil {
			return err
		}

		// Handle _index.md specially. Store metadata for the directory.
		if strings.HasSuffix(relPath, "_index.md") {
			dir := strings.TrimSuffix(relPath, "/_index.md")
			if dir == "_index.md" { // root _index.md
				dir = ""
			}
			dirMetadata[dir] = page
			return nil
		}

		// Insert regular pages into tree
		insertPage(tree, page, relPath, dirMetadata)
		return nil
	})

	if err != nil {
		return err
	}

	s.sortTree(tree)
	s.docsTree = tree
	return nil
}

func (s *DocsService) loadDocPage(fullPath, relPath string) (*model.DocPage, error) {
	content, err := fs.ReadFile(s.files, fullPath)
	if err != nil {
		return nil, err
	}
	content = s.withAppURL(content, relPath)

	htmlContent, meta, err := s.parser.ParseWithFrontmatter(content)
	if err != nil {
		return nil, err
	}

	// relPath already normalized to forward slashes
	slug := strings.TrimSuffix(relPath, ".md")

	// The markdown export shows surfaces blocks as plain lists and an
	// example as its code.
	plain := markdown.PlainSurfaces(content)
	if s.examples != nil {
		plain = markdown.PlainExamples(plain, s.examples)
	}

	page := &model.DocPage{
		Slug:        slug,
		Path:        relPath,
		HTMLContent: string(htmlContent),
		Content:     string(plain),
		Headings:    s.parser.ExtractHeadings(content),
		Children:    []*model.DocPage{},
	}

	title, ok := meta["title"].(string)
	if ok {
		page.Title = title
	} else {
		page.Title = titleFromSlug(slug)
	}

	if group, ok := meta["group"].(string); ok {
		page.Group = group
	}

	description, ok := meta["description"].(string)
	if ok {
		page.Description = description
	}

	order, ok := meta["order"].(int)
	if ok {
		page.Order = order
	} else {
		orderFloat, ok := meta["order"].(float64)
		if ok {
			page.Order = int(orderFloat)
		}
	}

	return page, nil
}

func insertPage(tree, page *model.DocPage, relPath string, dirMetadata map[string]*model.DocPage) {
	parts := strings.Split(relPath, "/") // Already normalized to forward slashes
	current := tree

	// Build/traverse the directory structure
	for i := 0; i < len(parts)-1; i++ {
		dirSlug := strings.Join(parts[:i+1], "/")

		// Check if this directory already exists
		var found *model.DocPage
		for _, child := range current.Children {
			if child.Slug == dirSlug {
				found = child
				break
			}
		}

		if found == nil {
			// Create new directory node
			dirPage := &model.DocPage{
				Slug:     dirSlug,
				Path:     dirSlug,
				Children: []*model.DocPage{},
				Parent:   current,
			}

			// Apply metadata from _index.md if available
			meta, ok := dirMetadata[dirSlug]
			if ok {
				dirPage.Title = meta.Title
				dirPage.Description = meta.Description
				dirPage.Order = meta.Order
				dirPage.HTMLContent = meta.HTMLContent
				dirPage.Content = meta.Content
				dirPage.Headings = meta.Headings
			} else {
				// Generate title from slug
				dirPage.Title = titleFromSlug(parts[i])
			}

			current.Children = append(current.Children, dirPage)
			current = dirPage
		} else {
			current = found
		}
	}

	// Add the page to its parent directory
	page.Parent = current
	current.Children = append(current.Children, page)
}

func (s *DocsService) sortTree(node *model.DocPage) {
	sort.Slice(node.Children, func(i, j int) bool {
		if node.Children[i].Order != node.Children[j].Order {
			return node.Children[i].Order < node.Children[j].Order
		}
		return node.Children[i].Title < node.Children[j].Title
	})

	for _, child := range node.Children {
		s.sortTree(child)
	}
}

func (s *DocsService) DocPage(slug string) (*model.DocPage, error) {
	if s.docsTree == nil {
		err := s.BuildDocsTree()
		if err != nil {
			return nil, err
		}
	}

	if slug == "" {
		// Find the first actual content page (not a directory)
		firstPage := s.findFirstContentPage(s.docsTree)
		if firstPage != nil {
			return firstPage, nil
		}
		// Fall back to tree root if no content pages
		return s.docsTree, nil
	}

	page := s.findPage(s.docsTree, slug)
	if page == nil {
		return nil, fmt.Errorf("documentation page not found: %s", slug)
	}

	return page, nil
}

func (s *DocsService) findFirstContentPage(node *model.DocPage) *model.DocPage {
	// Check children in order
	for _, child := range node.Children {
		// If this child has no children, it's a content page
		if len(child.Children) == 0 {
			return child
		}
		// Otherwise, recursively check this child's children
		page := s.findFirstContentPage(child)
		if page != nil {
			return page
		}
	}
	return nil
}

func (s *DocsService) findPage(node *model.DocPage, slug string) *model.DocPage {
	if node.Slug == slug {
		return node
	}

	for _, child := range node.Children {
		found := s.findPage(child, slug)
		if found != nil {
			return found
		}
	}

	return nil
}

func (s *DocsService) DocsTree() (*model.DocPage, error) {
	if s.docsTree == nil || s.live {
		err := s.BuildDocsTree()
		if err != nil {
			return nil, err
		}
	}
	return s.docsTree, nil
}

// docsGroups are the topics of the docs home, in their order. A page names
// its topic in its front matter (group); a page without one, or with
// another, goes under "More" at the end.
var docsGroups = []string{"Start", "Apps", "Every day", "Servers and data", "Team", "Reference"}

// uiGroups are the topics of the docs of moin UI. Start holds the pages with
// a tab of their own; the others are components, and Layout has a tab.
var uiGroups = []string{"Start", "Forms", "Lists", "Status", "Content", "Charts", "Overlays", "Page", "Layout"}

// Groups are the pages of the docs home: every page under its topic, the
// topics in the order of docsGroups, the pages by their order.
func (s *DocsService) Groups() []model.DocGroup {
	if s.docsTree == nil {
		return nil
	}
	var leaves []*model.DocPage
	s.collectPagesInOrder(s.docsTree, &leaves)
	groups := make([]model.DocGroup, len(s.groups))
	for i, title := range s.groups {
		groups[i].Title = title
	}
	var more []*model.DocPage
	for _, page := range leaves {
		at := -1
		for i, title := range s.groups {
			if page.Group == title {
				at = i
			}
		}
		if at < 0 {
			more = append(more, page)
			continue
		}
		groups[at].Pages = append(groups[at].Pages, page)
	}
	if len(more) > 0 {
		groups = append(groups, model.DocGroup{Title: "More", Pages: more})
	}
	kept := groups[:0]
	for _, group := range groups {
		sort.SliceStable(group.Pages, func(i, j int) bool { return group.Pages[i].Order < group.Pages[j].Order })
		if len(group.Pages) > 0 {
			kept = append(kept, group)
		}
	}
	return kept
}

// FlatDocsList is every page in the order of the docs home, so "Next" walks
// through the docs the way the home lists them.
func (s *DocsService) FlatDocsList() []*model.DocPage {
	var pages []*model.DocPage
	for _, group := range s.Groups() {
		pages = append(pages, group.Pages...)
	}
	return pages
}

// collectPagesInOrder collects every page that is no directory, in the
// order of the tree.
func (s *DocsService) collectPagesInOrder(node *model.DocPage, pages *[]*model.DocPage) {
	if node.Slug != "" && len(node.Children) == 0 {
		*pages = append(*pages, node)
	}
	for _, child := range node.Children {
		s.collectPagesInOrder(child, pages)
	}
}

// PrevNextPages returns the previous and next pages in the documentation flow
func (s *DocsService) PrevNextPages(currentPage *model.DocPage) (prev, next *model.DocPage) {
	pages := s.FlatDocsList()

	for i, page := range pages {
		if page.Slug == currentPage.Slug {
			if i > 0 {
				prev = pages[i-1]
			}
			if i < len(pages)-1 {
				next = pages[i+1]
			}
			break
		}
	}

	return prev, next
}

func titleFromSlug(slug string) string {
	parts := strings.Split(slug, "/")
	lastPart := parts[len(parts)-1]

	lastPart = strings.ReplaceAll(lastPart, "-", " ")
	lastPart = strings.ReplaceAll(lastPart, "_", " ")

	words := strings.Fields(lastPart)
	caser := cases.Title(language.English)
	for i, word := range words {
		words[i] = caser.String(word)
	}

	return strings.Join(words, " ")
}
