package components

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

var between = regexp.MustCompile(`>\s+<`)

// flat makes two pieces of HTML comparable: no space between tags.
func flat(html string) string {
	return strings.TrimSpace(between.ReplaceAllString(strings.Join(strings.Fields(html), " "), "><"))
}

func text(words string) templ.Component {
	return templ.Raw(words)
}

// TestComponentsPrintTheExamples: the HTML of a file in ui/examples is the
// contract. Each component must print exactly a piece of its example.
func TestComponentsPrintTheExamples(t *testing.T) {
	tests := []struct {
		example  string
		part     templ.Component
		children string
	}{
		{"preview", Preview(PreviewProps{ID: "demo-preview", Code: `<span class="moin-badge">Beta</span>`}), `<span class="moin-badge">Beta</span>`},
		{"code-with-copy", Code(CodeProps{ID: "demo-code-text", Text: "moin deploy my-app\nmoin logs my-app --follow"}), ""},
		{"footer", Footer(FooterProps{Columns: []FooterColumn{
			{Title: "Product", Links: []FooterLink{{Label: "Pricing", Href: "#"}, {Label: "Changelog", Href: "#"}}},
			{Title: "Company", Links: []FooterLink{{Label: "About", Href: "#"}, {Label: "Blog", Href: "https://example.com", External: true}}},
		}, End: text("<span>© 2026 Acme</span>")}), `<span class="moin-strong">Acme</span>`},
		{"button", Button(ButtonProps{Kind: Primary}), "Deploy"},
		{"button", Button(ButtonProps{}), "Restart"},
		{"button", Button(ButtonProps{Kind: Danger}), "Remove"},
		{"item-opens", Item(ItemProps{Href: "#", Name: "An item that opens a page"}), ""},
		{"item-opens", Item(ItemProps{Href: "#", Name: "With a second line", Note: "Only when there is something to say", Value: "value"}), ""},
		{"item-opens", Item(ItemProps{Href: "https://example.com", External: true, Name: "Another site", Note: "In a new tab"}), ""},
		{"item-does", Item(ItemProps{Button: true, Name: "Back up now"}), ""},
		{"item-tells", Item(ItemProps{Name: "Version", Value: "v1.0.0"}), ""},
		{"item-tells", Item(ItemProps{Name: "Last 24 hours", Note: "182 ms on average", Value: "99.98%"}), ""},
		{"status-word", Status(StatusProps{State: StateLive}), "live"},
		{"status-pill", Chip(ChipProps{State: StateFailed}), "v13 failed"},
		{"code-command", Command(CommandProps{Text: "moin deploy my-app"}), ""},
		{"tabs", Tabs(TabsProps{Label: "Sections", Tabs: []Tab{{Label: "App", Href: "#", Active: true}, {Label: "Deploys", Href: "#"}, {Label: "Domains", Href: "#"}, {Label: "Logs", Href: "#"}}}), ""},
		{"tabs-quiet", Tabs(TabsProps{Label: "Sections", Quiet: true, Tabs: []Tab{{Label: "App", Href: "#", Active: true}, {Label: "Deploys", Href: "#"}, {Label: "Domains", Href: "#"}, {Label: "Logs", Href: "#"}}}), ""},
		{"bar-floating", BarLinks(BarLinksProps{Label: "Acme", Links: []BarLink{{Label: "Docs", Href: "#"}, {Label: "Pricing", Href: "#", Current: true}}}), ""},
		{"switch-on-its-own", Switch(SwitchProps{Label: "On", Checked: true}), ""},
		{"switch-on-its-own", Switch(SwitchProps{Label: "Cannot change", Disabled: true}), ""},
	}
	for _, test := range tests {
		file, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "ui", "examples", test.example+".html"))
		if err != nil {
			t.Fatal(err)
		}
		var out strings.Builder
		ctx := templ.WithChildren(context.Background(), nil)
		if test.children != "" {
			ctx = templ.WithChildren(context.Background(), text(test.children))
		}
		if err := test.part.Render(ctx, &out); err != nil {
			t.Fatal(err)
		}
		if example, printed := flat(string(file)), flat(out.String()); !strings.Contains(example, printed) {
			t.Errorf("%s: printed\n%s\nis not in the example\n%s", test.example, printed, example)
		}
	}
}
