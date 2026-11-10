package components

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
)

var between = regexp.MustCompile(`>\s+<`)

// flat makes two pieces of HTML comparable: no space between tags.
func flat(html string) string {
	return strings.TrimSpace(between.ReplaceAllString(strings.Join(strings.Fields(html), " "), "><"))
}

// exampleLogo is the logo in the example of the app icon.
const exampleLogo = "/assets/ui/icons/package.svg"

func text(words string) templ.Component {
	return templ.Raw(words)
}

// exampleChart is the chart of the example: a pause, no value at one time
// and a mark.
func exampleChart() ChartProps {
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	clock := func(t time.Time) string { return t.Format("15:04") }
	return ChartProps{
		Times: []time.Time{at, at.Add(time.Minute), at.Add(2 * time.Minute), at.Add(3 * time.Minute), at.Add(10 * time.Minute), at.Add(11 * time.Minute)},
		Lines: []ChartLine{{Label: "CPU", Values: []float64{20, 35, math.NaN(), 30, 45, 40}}},
		Top:   100, Gap: 5 * time.Minute,
		Value: func(v float64) string { return fmt.Sprintf("%.0f%%", v) },
		Time:  clock, Tick: clock,
		Marks: map[int]string{3: "v12 went live"},
	}
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
		{"bento", Card(CardProps{Href: "#"}), `<span class="moin-strong">Deploy</span><span class="moin-small moin-muted">A tile that opens.</span>`},
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
		{"line-chart-with-marks", Chart(exampleChart()), ""},
		{"app-icon", AppIcon(AppIconProps{Logo: exampleLogo}), ""},
		{"app-icon", AppIcon(AppIconProps{Initial: "A"}), ""},
		{"app-icon", AppIcon(AppIconProps{Logo: exampleLogo, Large: true}), ""},
		{"app-icon", AppIcon(AppIconProps{Initial: "A", Large: true}), ""},
		{"expand-quiet", Expand(ExpandProps{Name: "Show details", Quiet: true}), `<p class="moin-text">Words that were not worth a place on the page, until you asked.</p>`},
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
