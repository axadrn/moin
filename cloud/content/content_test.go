package content

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/markdown"
)

// TestTheBinaryHoldsTheTexts: everything the site reads is in the binary, the
// files that start with an underscore too.
func TestTheBinaryHoldsTheTexts(t *testing.T) {
	texts := FS(false)
	for _, name := range []string{"blog/your-server-needs-no-open-port.md", "changelog.md", "llms.txt", "docs/getting-started.md", "legal/terms.md"} {
		if _, err := fs.Stat(texts, name); err != nil {
			t.Error(err)
		}
	}
}

// docsLink is a link to a docs page in Markdown, with its anchor if it has
// one: [Backups](/docs/backups#restore).
var docsLink = regexp.MustCompile(`\]\(/docs/([a-z0-9/-]+)(?:\.md)?(?:#([a-z0-9-]+))?\)`)

// TestDocsLinksLeadSomewhere: every link to a docs page, from the docs, the
// blog, the legal pages and llms.txt, names a page that exists, and a heading
// of it that exists.
func TestDocsLinksLeadSomewhere(t *testing.T) {
	texts := FS(false)
	parser := markdown.NewParser()
	anchors := map[string]map[string]bool{}
	page := func(slug string) (map[string]bool, bool) {
		if ids, ok := anchors[slug]; ok {
			return ids, true
		}
		source, err := fs.ReadFile(texts, "docs/"+slug+".md")
		if err != nil {
			return nil, false
		}
		ids := map[string]bool{}
		for _, heading := range parser.ExtractHeadings(source) {
			ids[heading.ID] = true
		}
		anchors[slug] = ids
		return ids, true
	}
	err := fs.WalkDir(texts, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !(strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".txt")) || name == "changelog.md" {
			return err
		}
		source, err := fs.ReadFile(texts, name)
		if err != nil {
			return err
		}
		for _, link := range docsLink.FindAllStringSubmatch(string(source), -1) {
			ids, ok := page(link[1])
			if !ok {
				t.Errorf("%s links to /docs/%s, which is no page", name, link[1])
				continue
			}
			if link[2] != "" && !ids[link[2]] {
				t.Errorf("%s links to /docs/%s#%s, which is no heading of it", name, link[1], link[2])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
