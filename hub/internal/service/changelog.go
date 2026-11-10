package service

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/githubstars"
	"github.com/axadrn/moin/hub/internal/markdown"
)

// issueAtEnd is a "(#N)" that ends a list item of the changelog. The GitHub
// release of a version has the same text, so the file keeps a plain "(#N)",
// which GitHub links by itself, and this page links it here.
var issueAtEnd = regexp.MustCompile(`(?m)^(- .*) \(#(\d+)\)$`)

// Release is one version of the changelog: its version, its date, its
// one-line summary, the "In short" line, as written and as HTML, and the rest
// of its notes as HTML.
type Release struct {
	Version     string
	Date        time.Time
	InShort     string
	InShortHTML string
	Notes       string
}

// Anchor is the place of this version on the changelog page.
func (r Release) Anchor() string {
	return strings.ReplaceAll(r.Version, ".", "")
}

// ChangelogReleases reads the versions of changelog.md, newest first, for
// the changelog page and the feed.
func ChangelogReleases(files fs.FS) ([]Release, error) {
	source, err := fs.ReadFile(files, "changelog.md")
	if err != nil {
		return nil, fmt.Errorf("read changelog: %w", err)
	}
	var releases []Release
	var notes [][]byte
	scanner := bufio.NewScanner(bytes.NewReader(source))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "## v"):
			releases = append(releases, Release{Version: strings.TrimPrefix(line, "## ")})
			notes = append(notes, nil)
		case len(releases) == 0:
		case releases[len(releases)-1].Date.IsZero():
			if date, err := time.Parse("January 2, 2006", line); err == nil {
				releases[len(releases)-1].Date = date
			}
		case strings.HasPrefix(line, "**In short:**") && releases[len(releases)-1].InShort == "":
			releases[len(releases)-1].InShort = strings.TrimSpace(strings.TrimPrefix(line, "**In short:**"))
			releases[len(releases)-1].InShortHTML = markdown.Inline(releases[len(releases)-1].InShort)
		case releases[len(releases)-1].InShort != "":
			notes[len(notes)-1] = append(append(notes[len(notes)-1], scanner.Bytes()...), '\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read changelog: %w", err)
	}
	for i := range notes {
		notes[i] = issueAtEnd.ReplaceAll(notes[i], []byte("$1 ([#$2](https://github.com/"+githubstars.Repo+"/issues/$2))"))
	}
	html, err := markdown.NewParser().ParseParts(notes)
	if err != nil {
		return nil, fmt.Errorf("render changelog: %w", err)
	}
	for i := range releases {
		releases[i].Notes = string(html[i])
	}
	return releases, nil
}
