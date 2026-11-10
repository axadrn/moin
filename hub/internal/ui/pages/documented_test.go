package pages

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	classWord = regexp.MustCompile(`moin-[a-z0-9]+(?:-[a-z0-9]+)*`)
	dataWord  = regexp.MustCompile(`data-[a-z0-9-]+(?:="[^"{]*")?`)
	styleHook = regexp.MustCompile(`\[(data-[a-z0-9-]+)`)
)

// notKit look like a class of moin UI and are not one: keys of localStorage,
// URL params and custom elements.
var notKit = map[string]bool{
	"moin-mode":           true,
	"moin-docs-surface":   true,
	"moin-changelog-seen": true,
	"moin-theme-editor":   true,
}

func readAll(t *testing.T, keep func(string) bool, roots ...string) string {
	t.Helper()
	var all strings.Builder
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !keep(path) {
				return err
			}
			source, err := os.ReadFile(path)
			all.Write(source)
			all.WriteByte('\n')
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return all.String()
}

// withoutComments drops whole-line Go comments, which may name a class that
// is gone.
func withoutComments(source string) string {
	var kept strings.Builder
	for line := range strings.SplitSeq(source, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			kept.WriteString(line + "\n")
		}
	}
	return kept.String()
}

// TestHubUsesOnlyDocumentedParts: every moin-* class and every data-*
// attribute of the layout in the hub's templates must be in an example, a
// block or a docs page of moin UI. What the site uses, anyone can use.
func TestHubUsesOnlyDocumentedParts(t *testing.T) {
	source := func(path string) bool {
		return strings.HasSuffix(path, ".templ") ||
			strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_templ.go") && !strings.HasSuffix(path, "_test.go")
	}
	hub := withoutComments(readAll(t, source, ".", "../layouts", "../components"))
	kit := "../../../../ui/"
	documented := readAll(t, func(string) bool { return true }, kit+"examples", kit+"blocks", kit+"docs")
	styles := readAll(t, func(string) bool { return true }, kit+"layout.css", kit+"components/text.css")

	known := map[string]bool{}
	for _, word := range append(classWord.FindAllString(documented, -1), dataWord.FindAllString(documented, -1)...) {
		known[word] = true
		if name, _, valued := strings.Cut(word, "="); valued {
			known[name] = true
		}
	}
	hooks := map[string]bool{}
	for _, match := range styleHook.FindAllStringSubmatch(styles, -1) {
		hooks[match[1]] = true
	}

	missing := map[string]bool{}
	for _, class := range classWord.FindAllString(hub, -1) {
		if !known[class] && !notKit[class] {
			missing[class] = true
		}
	}
	for _, word := range dataWord.FindAllString(hub, -1) {
		name, _, _ := strings.Cut(word, "=")
		if hooks[name] && !known[word] {
			missing[word] = true
		}
	}
	for word := range missing {
		t.Errorf("%s is used by the hub, but no example, block or docs page of moin UI has it", word)
	}
}
