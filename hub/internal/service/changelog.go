package service

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/markdown"
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

// Release is one version of the changelog: its version, its date and its
// one-line summary, the "In short" line.
type Release struct {
	Version string
	Date    time.Time
	InShort string
}

// ChangelogReleases reads the versions of <contentDir>/changelog.md, newest
// first, for the feed.
func ChangelogReleases(contentDir string) ([]Release, error) {
	source, err := os.ReadFile(filepath.Join(contentDir, "changelog.md"))
	if err != nil {
		return nil, fmt.Errorf("read changelog: %w", err)
	}
	var releases []Release
	scanner := bufio.NewScanner(bytes.NewReader(source))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "## v"):
			releases = append(releases, Release{Version: strings.TrimPrefix(line, "## ")})
		case len(releases) == 0:
		case releases[len(releases)-1].Date.IsZero():
			if date, err := time.Parse("January 2, 2006", line); err == nil {
				releases[len(releases)-1].Date = date
			}
		case strings.HasPrefix(line, "**In short:**") && releases[len(releases)-1].InShort == "":
			releases[len(releases)-1].InShort = strings.TrimSpace(strings.TrimPrefix(line, "**In short:**"))
		}
	}
	return releases, scanner.Err()
}
