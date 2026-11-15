package pages

import (
	"strings"
	"testing"
)

// New hook asks first only when it replaces a hook, and sits in the
// part that re-renders, so it knows after the first hook.
func TestNewHookAsksOnlyToReplace(t *testing.T) {
	for _, on := range []bool{false, true} {
		var html strings.Builder
		if err := AppHook("shop", "shop-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b", "", "docker.io/me/shop", on).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
			t.Fatal(err)
		}
		page := html.String()
		if !strings.Contains(page, "data-new-hook") {
			t.Fatalf("hook %v: no New hook button: %s", on, page)
		}
		if asks := strings.Contains(page, "The old URL stops working"); asks != on {
			t.Errorf("hook %v: asks %v", on, asks)
		}
	}
}

// A hook says the image it deploys new tags of, the one running now, and
// its curl uses it; without an image running, it says to deploy one first.
func TestHookShowsItsImage(t *testing.T) {
	for image, want := range map[string]string{
		"docker.io/me/shop": `New tags of <code class="moin-mono moin-foreground">docker.io/me/shop</code>`,
		"":                  "Waits for an image",
	} {
		var html strings.Builder
		if err := AppHook("shop", "shop-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b", "https://moin.example.com/webhooks/deploy/dph_x", image, true).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
			t.Fatal(err)
		}
		page := html.String()
		if !strings.Contains(page, want) || !strings.Contains(page, "Keep it secret, like a password.") {
			t.Errorf("image %q: %s", image, page)
		}
		if image != "" && !strings.Contains(page, "?image=docker.io/me/shop:$GITHUB_SHA") {
			t.Errorf("curl without the image: %s", page)
		}
	}
}
