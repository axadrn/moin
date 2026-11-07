package pages

import (
	"strings"
	"testing"
)

// The add form comes with the next free name filled in.
func TestServerAddFormSuggestsTheName(t *testing.T) {
	var html strings.Builder
	if err := ServerAddForm("web-2").Render(projectPageContext("/app/tiny-rocket-co-workspace1/servers"), &html); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), `value="web-2"`) {
		t.Fatalf("no suggested name in the form:\n%s", html.String())
	}
}
