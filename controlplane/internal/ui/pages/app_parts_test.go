package pages

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// A card names its guide with a small book in its header, a ghost icon
// button with a tooltip, never a text link that makes the card noisy.
func TestCardDocsIsABook(t *testing.T) {
	for _, action := range []templ.Component{nil, templ.Raw(`<button data-action>Change</button>`)} {
		var html strings.Builder
		if err := panel(panelProps{Title: "GitHub", Docs: "/docs/guides/github", Action: action}).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
			t.Fatal(err)
		}
		page := html.String()
		at := strings.Index(page, `data-docs="/docs/guides/github"`)
		if at < 0 {
			t.Fatalf("action %v: no book: %s", action != nil, page)
		}
		book := page[strings.LastIndex(page[:at], "<a"):]
		book = book[:strings.Index(book, "</a>")]
		for _, want := range []string{`aria-label="How it works"`, `data-slot="tooltip-trigger"`, "size-8", "text-muted-foreground", "<svg"} {
			if !strings.Contains(book, want) {
				t.Errorf("action %v: the book has no %s: %s", action != nil, want, book)
			}
		}
		if strings.Contains(book[strings.Index(book, ">"):], "How it works") {
			t.Errorf("action %v: the book shows its label as text: %s", action != nil, book)
		}
		if !strings.Contains(page, `data-slot="tooltip-content"`) {
			t.Errorf("action %v: no tooltip: %s", action != nil, page)
		}
		// Only an Action moves under the title on phones.
		if stacks := strings.Contains(page, "max-sm:col-start-1"); stacks != (action != nil) {
			t.Errorf("action %v: stacks on phones %v", action != nil, stacks)
		}
	}
}
