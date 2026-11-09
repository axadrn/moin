package parts

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

// TestPartsPrintTheKitsHTML: the HTML of an example in kit/examples is the
// contract. Each part must print exactly a piece of its example.
func TestPartsPrintTheKitsHTML(t *testing.T) {
	tests := []struct {
		example  string
		part     templ.Component
		children string
	}{
		{"button", Button(ButtonProps{Kind: Primary}), "Deploy"},
		{"button", Button(ButtonProps{}), "Restart"},
		{"button", Button(ButtonProps{Kind: Danger}), "Remove"},
		{"item-opens", Item(ItemProps{Href: "#kit", Name: "An item that opens a page"}), ""},
		{"item-opens", Item(ItemProps{Href: "#kit", Name: "With a second line", Note: "Only when there is something to say", Value: "value"}), ""},
		{"item-opens", Item(ItemProps{Href: "https://example.com", External: true, Name: "Another site", Note: "In a new tab"}), ""},
		{"item-does", Item(ItemProps{Button: true, Name: "Back up now"}), ""},
		{"item-tells", Item(ItemProps{Name: "Version", Value: "v1.0.0"}), ""},
		{"item-tells", Item(ItemProps{Name: "Last 24 hours", Note: "182 ms on average", Value: "99.98%"}), ""},
		{"status-word", Status(StatusProps{State: StateLive}), "live"},
		{"status-pill", Chip(ChipProps{State: StateFailed}), "v13 failed"},
		{"code-command", Command(CommandProps{Text: "moin deploy my-app"}), ""},
		{"tabs", Tabs(TabsProps{Label: "Sections", Tabs: []Tab{{Label: "App", Href: "#kit", Active: true}, {Label: "Deploys", Href: "#kit"}, {Label: "Domains", Href: "#kit"}, {Label: "Logs", Href: "#kit"}}}), ""},
		{"tabs-quiet", Tabs(TabsProps{Label: "Sections", Quiet: true, Tabs: []Tab{{Label: "App", Href: "#kit", Active: true}, {Label: "Deploys", Href: "#kit"}, {Label: "Domains", Href: "#kit"}, {Label: "Logs", Href: "#kit"}}}), ""},
		{"bar-floating", BarLinks(BarLinksProps{Label: "Acme", Links: []BarLink{{Label: "Docs", Href: "#kit"}, {Label: "Pricing", Href: "#kit", Current: true}}}), ""},
		{"switch-on-its-own", Switch(SwitchProps{Label: "On", Checked: true}), ""},
		{"switch-on-its-own", Switch(SwitchProps{Label: "Cannot change", Disabled: true}), ""},
	}
	for _, test := range tests {
		file, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "kit", "examples", test.example+".html"))
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
