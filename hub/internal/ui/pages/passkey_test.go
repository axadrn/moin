package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
)

func renderPage(t *testing.T, ctx context.Context, page templ.Component) string {
	t.Helper()
	var output bytes.Buffer
	if err := page.Render(ctx, &output); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

// The sign-in page offers a passkey on every hub with a domain, next to
// GitHub or the recovery key.
func TestAuthOffersAPasskey(t *testing.T) {
	for _, github := range []bool{true, false} {
		ctx := ctxkeys.WithRenderConfig(context.Background(), config.RenderConfig{GitHubLogin: github, Passkeys: true})
		page := renderPage(t, ctx, Auth("", ""))
		for _, want := range []string{"Sign in with a passkey", `data-passkey="sign-in"`, `data-passkey-url="/auth/passkey"`, "data-passkey-error", "js/passkey.js"} {
			if !strings.Contains(page, want) {
				t.Fatalf("GitHub %v: the sign-in page lacks %s", github, want)
			}
		}
		if strings.Contains(page, `name="key"`) == github {
			t.Fatalf("GitHub %v: the key field shows only without the GitHub App", github)
		}
	}
	ctx := ctxkeys.WithRenderConfig(context.Background(), config.RenderConfig{GitHubLogin: true})
	if page := renderPage(t, ctx, Auth("", "")); strings.Contains(page, "data-passkey") {
		t.Fatal("a hub without passkeys offers one")
	}
}

// The security tab lists the passkeys, each with Rename and Remove, and
// adds one.
func TestSettingsListsPasskeys(t *testing.T) {
	ctx := projectPageContext("/app/tiny-rocket-co/settings/account")
	ctx = ctxkeys.WithRenderConfig(ctx, config.RenderConfig{Passkeys: true})
	used := time.Now().Add(-2 * time.Hour)
	passkeys := []model.Passkey{
		{ID: "p1", Name: "Passkey", CreatedAt: time.Now().Add(-72 * time.Hour), LastUsedAt: &used},
		{ID: "p2", Name: "Passkey", CreatedAt: time.Now()},
	}
	page := renderPage(t, ctx, Settings(nil, "", passkeys, "", "security"))
	for _, want := range []string{"Passkeys", "Added 3d ago · used 2h ago", "Added just now · never used", `href="/app/tiny-rocket-co/settings/account?tab=security&amp;passkey=p1"`, `hx-delete="/app/tiny-rocket-co/settings/passkeys/p1"`, "Add passkey", `data-passkey="add"`, `data-passkey-url="/app/tiny-rocket-co/settings/passkeys"`, "js/passkey.js"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the security tab lacks %s", want)
		}
	}
}
