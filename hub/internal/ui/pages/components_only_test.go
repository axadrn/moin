package pages

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var old = []struct {
	what string
	re   *regexp.Regexp
}{
	{"the old kit package", regexp.MustCompile(`\bkit\.[A-Z]`)},
	{"a control written by hand", regexp.MustCompile(`<(button|select|textarea)\b|<input\b[^>]*type="(text|email|password|search|checkbox|radio|number|url)"`)},
	{"the class of a component", regexp.MustCompile(`class="(?:[^"]*\s)?moin-(button|input|textarea|select|switch|choice|item|tab|tabs|status|chip|dot|command|dialog|avatar|header)(?:\s[^"]*)?"`)},
}

// TestPagesUseComponents: every control and every component with markup of
// its own comes from the components package, so one place decides what it
// looks like. Layout, text, a plain card or list box stay classes.
func TestPagesUseComponents(t *testing.T) {
	pages, err := filepath.Glob("*.templ")
	if err != nil || len(pages) == 0 {
		t.Fatal("no pages", err)
	}
	for _, page := range pages {
		source, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		for _, rule := range old {
			if found := rule.re.Find(source); found != nil {
				t.Errorf("%s has %s: %s", page, rule.what, found)
			}
		}
	}
}
