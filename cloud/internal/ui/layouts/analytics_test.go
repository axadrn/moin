package layouts

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/axadrn/moin/cloud/internal/config"
	"github.com/axadrn/moin/cloud/internal/ctxkeys"
)

// Every page carries the page counter when Umami is set.
func TestPagesCount(t *testing.T) {
	ctx := ctxkeys.WithRenderConfig(context.Background(), config.RenderConfig{UmamiWebsiteID: "site", UmamiHost: "umami.example.com"})
	var out bytes.Buffer
	if err := Home(SEOProps{Title: "moin"}).Render(ctx, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "data-website-id") {
		t.Error("the site does not count")
	}
}
