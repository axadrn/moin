package pages

import (
	"regexp"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

// New hook asks first only when it replaces a hook or unlinks the repo, and
// sits in the card that re-renders, so it knows after the first hook.
func TestNewHookAsksOnlyToReplace(t *testing.T) {
	hash := "hash"
	for _, test := range []struct {
		app  model.App
		asks string
	}{
		{model.App{Name: "shop"}, ""},
		{model.App{Name: "shop", DeployHookHash: &hash}, "The old URL stops working"},
		{model.App{Name: "shop", Repo: "me/shop"}, "It unlinks me/shop"},
	} {
		var html strings.Builder
		if err := appHook(test.app, "").Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
			t.Fatal(err)
		}
		page := html.String()
		if !strings.Contains(page, "data-new-hook") {
			t.Fatalf("%+v: no New hook button: %s", test.app, page)
		}
		if asks := strings.Contains(page, "hx-confirm"); asks != (test.asks != "") || !strings.Contains(page, test.asks) {
			t.Errorf("%+v: asks %v: %s", test.app, asks, page)
		}
	}
}

// A hook says the image it deploys new tags of, the one running now, or
// that it waits for one; a new hook's URL comes with the note to keep it
// secret and guesses no registry or CI.
func TestHookShowsItsImage(t *testing.T) {
	hash := "hash"
	for image, want := range map[string]string{
		"docker.io/me/shop:v1": `New tags of <code class="moin-mono moin-foreground">docker.io/me/shop</code>`,
		"":                     "Waits for an image",
	} {
		var html strings.Builder
		app := model.App{Name: "shop", LiveImage: image, DeployHookHash: &hash}
		if err := appHook(app, "https://moin.example.com/webhooks/deploy/dph_x").Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
			t.Fatal(err)
		}
		page := html.String()
		if !strings.Contains(page, want) || !strings.Contains(page, "Keep it secret, like a password.") || strings.Contains(page, "ghcr.io") || strings.Contains(page, "GITHUB") {
			t.Errorf("image %q: %s", image, page)
		}
	}
}

// Who brings new versions is one answer: the one the app has now is
// picked. The link form picks no repo before you do, and asks first when
// linking turns the deploy hook off.
func TestSourceIsOneAnswer(t *testing.T) {
	hash := "hash"
	picked := regexp.MustCompile(`<input[^>]*\bchecked\b[^>]*data-source="(\w+)"`)
	repos := []service.GitHubRepoOf{{GitHubRepo: service.GitHubRepo{FullName: "me/shop"}}}
	for want, app := range map[string]model.App{
		"hand":   {Name: "shop"},
		"github": {Name: "shop", Repo: "me/shop", Branch: "main"},
		"ci":     {Name: "shop", DeployHookHash: &hash},
	} {
		var html strings.Builder
		if err := AppSource(RepoView{App: app, Enabled: true, Connected: true, Repos: repos}).Render(projectPageContext("/app/tiny-rocket-co/apps/shop/deploys"), &html); err != nil {
			t.Fatal(err)
		}
		page := html.String()
		if found := picked.FindAllStringSubmatch(page, -1); len(found) != 1 || found[0][1] != want {
			t.Errorf("%s: picked %v", want, found)
		}
		if app.Repo == "" && !strings.Contains(page, "Pick a repo") {
			t.Errorf("%s: the link form picked a repo: %s", want, page)
		}
		if asks := strings.Contains(page, "Link a repo? The deploy hook turns off."); asks != (want == "ci") {
			t.Errorf("%s: link asks %v", want, asks)
		}
	}
}
