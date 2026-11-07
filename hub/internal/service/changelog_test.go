package service

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestChangelogFormat keeps every release in the release notes format of
// AGENTS.md: a date, the In short line, then New, Changed, Fixed, Removed
// and Upgrade in this order, Upgrade always there.
func TestChangelogFormat(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "content", "changelog.md"))
	if err != nil {
		t.Fatal(err)
	}
	order := []string{"Why", "New", "Changed", "Fixed", "Removed", "Upgrade"}
	releases := strings.Split(string(source), "\n## ")[1:]
	if len(releases) == 0 {
		t.Fatal("no releases")
	}
	for _, release := range releases {
		lines := strings.Split(strings.TrimSpace(release), "\n")
		version := lines[0]
		if !strings.HasPrefix(version, "v") || len(lines) < 5 {
			t.Errorf("%q: not a version with notes", version)
			continue
		}
		if _, err := time.Parse("January 2, 2006", lines[2]); err != nil {
			t.Errorf("%s: the line under the version is no date like November 3, 2026: %q", version, lines[2])
		}
		if !strings.HasPrefix(lines[4], "**In short:** ") {
			t.Errorf("%s: does not start with **In short:**", version)
		}
		last := -1
		var sections []string
		for _, line := range lines {
			heading, ok := strings.CutPrefix(line, "### ")
			if !ok {
				continue
			}
			index := slices.Index(order, heading)
			if index <= last {
				t.Errorf("%s: section %q is unknown or out of order", version, heading)
			}
			last = max(last, index)
			sections = append(sections, heading)
		}
		if !slices.Contains(sections, "Upgrade") {
			t.Errorf("%s: no Upgrade section", version)
		}
	}
	html, err := Changelog(filepath.Join("..", "..", "content"))
	if err != nil || !strings.Contains(html, "v1.0.0-beta.1") {
		t.Errorf("Changelog renders %d bytes, %v", len(html), err)
	}
}

// TestChangelogReleases: the feed reads every version with its date and
// its In short line, newest first.
func TestChangelogReleases(t *testing.T) {
	releases, err := ChangelogReleases("../../content")
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) < 10 {
		t.Fatalf("%d releases", len(releases))
	}
	for _, release := range releases {
		if release.Date.IsZero() || release.InShort == "" {
			t.Errorf("%s: date %v, in short %q", release.Version, release.Date, release.InShort)
		}
	}
	if !releases[0].Date.After(releases[len(releases)-1].Date) && releases[0].Date != releases[len(releases)-1].Date {
		t.Errorf("not newest first: %v … %v", releases[0].Date, releases[len(releases)-1].Date)
	}
}
