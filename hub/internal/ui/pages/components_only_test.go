package pages

import (
	"os"
	"regexp"
	"testing"
)

// onParts are the pages already on the kit. A page joins this list when it
// moves, and at the end the list is every page.
var onParts = []string{
	"app_settings.templ",
	"settings_api_tokens.templ",
	"settings_avatar.templ",
	"settings_home.templ",
}

var notOnParts = []struct {
	what string
	re   *regexp.Regexp
}{
	{"the old kit package", regexp.MustCompile(`@kit\.[A-Z]`)},
	{"a control written by hand", regexp.MustCompile(`<(button|select|textarea)\b|<input\b[^>]*type="(text|email|password|search|checkbox|radio|number|url)"`)},
	{"the class of a part", regexp.MustCompile(`class="[^"]*\bkit-(button|input|textarea|select|switch|choice|list|item|card|tab|tabs|status|chip|dot|command|dialog|avatar|header)\b`)},
}

// TestPagesUseParts: on these pages every control and part comes from the
// parts package, so one place decides what it looks like. Layout and text
// stay plain classes.
func TestPagesUseParts(t *testing.T) {
	for _, page := range onParts {
		source, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		for _, rule := range notOnParts {
			if found := rule.re.Find(source); found != nil {
				t.Errorf("%s has %s: %s", page, rule.what, found)
			}
		}
	}
}
