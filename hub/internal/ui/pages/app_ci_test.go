package pages

import (
	"strings"
	"testing"
)

// New deploy hook asks first only when it replaces a hook, and sits in the
// part that re-renders, so it knows after the first hook.
func TestNewHookAsksOnlyToReplace(t *testing.T) {
	for _, on := range []bool{false, true} {
		var html strings.Builder
		if err := AppHook("shop", "shop-0f8e2c4a1b3d4e5f8a9b0c1d2e3f4a5b", "", on).Render(projectPageContext("/app/tiny-rocket-co-workspace1/apps/shop"), &html); err != nil {
			t.Fatal(err)
		}
		page := html.String()
		if !strings.Contains(page, "data-new-hook") {
			t.Fatalf("hook %v: no New deploy hook button: %s", on, page)
		}
		if asks := strings.Contains(page, "The old URL stops working"); asks != on {
			t.Errorf("hook %v: asks %v", on, asks)
		}
	}
}
