package layouts

import (
	"context"
	"strings"
	"testing"

	"github.com/axadrn/moin/cloud/internal/ctxkeys"
)

// TestNavbarSession shows the way back to moin cloud only when signed in.
func TestNavbarSession(t *testing.T) {
	render := func(in bool) string {
		var b strings.Builder
		ctx := ctxkeys.WithSignedIn(context.Background(), in)
		if err := Navbar().Render(ctx, &b); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	if out := render(true); !strings.Contains(out, `href="/cloud"`) || strings.Contains(out, "Get started") || strings.Contains(out, "Sign in") {
		t.Error("signed in: want a link to /cloud, no Get started, no Sign in")
	}
	if out := render(false); strings.Contains(out, `href="/cloud"`) || !strings.Contains(out, "Get started") || !strings.Contains(out, `href="/login"`) {
		t.Error("signed out: want Get started and no link to /cloud")
	}
}
