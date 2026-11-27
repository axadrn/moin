package service

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/axadrn/moin/hub/internal/markdown"
)

// LegalPage is the terms or the privacy policy, from legal/<slug>.md.
type LegalPage struct {
	Title       string
	Description string
	Slug        string
	Content     string
	// LastUpdated is the date as the file writes it, like November 25, 2026.
	LastUpdated string
}

var legalSlug = regexp.MustCompile(`^[a-z-]+$`)

// ReadLegalPage reads one legal page. {{support_email}} in it becomes
// supportEmail.
func ReadLegalPage(files fs.FS, slug, supportEmail string) (*LegalPage, error) {
	if !legalSlug.MatchString(slug) {
		return nil, fmt.Errorf("no legal page %q", slug)
	}
	source, err := fs.ReadFile(files, path.Join("legal", slug+".md"))
	if err != nil {
		return nil, err
	}
	source = []byte(strings.ReplaceAll(string(source), "{{support_email}}", supportEmail))
	html, meta, err := markdown.NewParser().ParseWithFrontmatter(source)
	if err != nil {
		return nil, err
	}
	text := func(key string) string {
		value, _ := meta[key].(string)
		return value
	}
	return &LegalPage{
		Title:       text("title"),
		Description: text("description"),
		Slug:        slug,
		Content:     string(html),
		LastUpdated: text("lastUpdated"),
	}, nil
}
