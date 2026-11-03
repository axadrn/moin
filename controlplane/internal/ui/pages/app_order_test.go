package pages

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/model"
)

func renderWithView(t *testing.T, component templ.Component, apps, servers string) string {
	t.Helper()
	ctx := projectPageContext("/app/tiny-rocket-co/apps")
	ctx = ctxkeys.WithUser(ctx, &model.User{ID: "user-1", AppsView: apps, ServersView: servers})
	var html strings.Builder
	if err := component.Render(ctx, &html); err != nil {
		t.Fatal(err)
	}
	return html.String()
}

// Both views of both pages keep the order they are given, carry it in the
// form that saves a new one, and give each item a grip.
func TestOrderedListsInBothViews(t *testing.T) {
	apps := AppsView{Apps: []model.App{{ID: "a1", Name: "shop"}, {ID: "a2", Name: "blog"}}}
	servers := []model.Server{{ID: "s1", Name: "web"}, {ID: "s2", Name: "db"}}
	for _, test := range []struct {
		name      string
		component templ.Component
		apps      string
		servers   string
		first     string
		second    string
		view      string
	}{
		{"apps as a list", AppList(apps), "", "", "shop", "blog", `data-view-switch="list"`},
		{"apps as a grid", AppList(apps), "grid", "", "shop", "blog", `data-view-switch="grid"`},
		{"servers as a grid", ServerList(servers), "", "", "web", "db", `data-view-switch="grid"`},
		{"servers as a list", ServerList(servers), "", "list", "web", "db", `data-view-switch="list"`},
	} {
		page := renderWithView(t, test.component, test.apps, test.servers)
		if !strings.Contains(page, test.view) {
			t.Errorf("%s: no %s", test.name, test.view)
		}
		if !strings.Contains(page, "data-sortable-list") || !strings.Contains(page, `hx-trigger="end, submit"`) {
			t.Errorf("%s: no sortable form", test.name)
		}
		first := strings.Index(page, `name="order" value="`+test.first+`"`)
		second := strings.Index(page, `name="order" value="`+test.second+`"`)
		if first < 0 || second < first {
			t.Errorf("%s: order inputs at %d and %d", test.name, first, second)
		}
		if strings.Count(page, "data-drag-handle") != 2 {
			t.Errorf("%s: %d grips, want 2", test.name, strings.Count(page, "data-drag-handle"))
		}
	}
}
