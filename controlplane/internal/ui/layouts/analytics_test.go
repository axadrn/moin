package layouts

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/axadrn/moin/controlplane/internal/config"
	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
)

// TestOnlyPublicPagesCount: the public layout carries the page counter, the
// app and sign-in layouts never do.
func TestOnlyPublicPagesCount(t *testing.T) {
	ctx := ctxkeys.WithRenderConfig(context.Background(), config.RenderConfig{UmamiWebsiteID: "site", UmamiHost: "https://umami.example.com"})
	render := func(component templ.Component) string {
		var out bytes.Buffer
		if err := component.Render(ctx, &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if !strings.Contains(render(Home(SEOProps{Title: "moin"})), "data-website-id") {
		t.Error("the public layout does not count")
	}
	if strings.Contains(render(Auth(SEOProps{Title: "Sign in"})), "data-website-id") {
		t.Error("the sign-in layout counts")
	}
	if strings.Contains(render(Base(SEOProps{Title: "app"})), "data-website-id") {
		t.Error("a page that is not public counts")
	}
}
