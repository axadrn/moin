package service

import (
	"os"
	"path/filepath"
	"regexp"
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
}

// TestChangelogLinksIssues: only a "(#N)" at the end of a list item of the
// notes becomes a link, and the real changelog has them.
func TestChangelogLinksIssues(t *testing.T) {
	dir := t.TempDir()
	source := "---\ntitle: Changelog\n---\n\n## v1\n\nNovember 3, 2026\n\n**In short:** Fast (#6).\n\n### New\n- **Done**: shipped. (#49)\n- **Middle**: see (#7) here.\n- **Bare**: see #8\n\nNot a list item (#9)\n"
	if err := os.WriteFile(filepath.Join(dir, "changelog.md"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	releases, err := ChangelogReleases(os.DirFS(dir))
	if err != nil || len(releases) != 1 {
		t.Fatal(len(releases), err)
	}
	html := releases[0].Notes
	if want := `shipped. (<a href="https://github.com/axadrn/moin/issues/49">#49</a>)</li>`; !strings.Contains(html, want) {
		t.Errorf("no link to the issue in %s", html)
	}
	for _, text := range []string{"(#7) here", "see #8", "list item (#9)"} {
		if !strings.Contains(html, text) {
			t.Errorf("%q should stay plain in %s", text, html)
		}
	}
	if strings.Contains(html, "In short") || strings.Contains(html, "November 3") {
		t.Errorf("the notes repeat what stands above them: %s", html)
	}
	if releases[0].InShort != "Fast (#6)." {
		t.Errorf("in short = %q", releases[0].InShort)
	}
	all, err := ChangelogReleases(os.DirFS(filepath.Join("..", "..", "content")))
	if err != nil {
		t.Fatal(err)
	}
	var page strings.Builder
	for _, release := range all {
		page.WriteString(release.Notes)
	}
	if !strings.Contains(page.String(), `(<a href="https://github.com/axadrn/moin/issues/17">#17</a>)</li>`) {
		t.Error("the changelog links no #17")
	}
}

// TestChangelogNotesHaveOneIDEach: every version has a New section, and they
// all stand on one page.
func TestChangelogNotesHaveOneIDEach(t *testing.T) {
	releases, err := ChangelogReleases(os.DirFS(filepath.Join("..", "..", "content")))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, release := range releases {
		for _, id := range regexp.MustCompile(`id="([^"]+)"`).FindAllStringSubmatch(release.Notes, -1) {
			if seen[id[1]] {
				t.Errorf("%s: id %q is used twice", release.Version, id[1])
			}
			seen[id[1]] = true
		}
	}
}

// TestChangelogReleases: the feed reads every version with its date and
// its In short line, newest first.
func TestChangelogReleases(t *testing.T) {
	releases, err := ChangelogReleases(os.DirFS(filepath.Join("..", "..", "content")))
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) < 10 {
		t.Fatalf("%d releases", len(releases))
	}
	for _, release := range releases {
		if release.Date.IsZero() || release.InShort == "" || release.Notes == "" {
			t.Errorf("%s: date %v, in short %q, %d bytes of notes", release.Version, release.Date, release.InShort, len(release.Notes))
		}
	}
	if !releases[0].Date.After(releases[len(releases)-1].Date) && releases[0].Date != releases[len(releases)-1].Date {
		t.Errorf("not newest first: %v … %v", releases[0].Date, releases[len(releases)-1].Date)
	}
}
