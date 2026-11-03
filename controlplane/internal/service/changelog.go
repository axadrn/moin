package service

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/axadrn/ops/controlplane/internal/markdown"
)

// Changelog renders <contentDir>/changelog.md, the release notes of every
// version, newest first. The GitHub release of a version has the same text.
func Changelog(contentDir string) (string, error) {
	source, err := os.ReadFile(filepath.Join(contentDir, "changelog.md"))
	if err != nil {
		return "", fmt.Errorf("read changelog: %w", err)
	}
	html, _, err := markdown.NewParser().ParseWithFrontmatter(source)
	if err != nil {
		return "", fmt.Errorf("render changelog: %w", err)
	}
	return string(html), nil
}
