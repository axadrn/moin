package pages

import (
	"cmp"
	"regexp"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

// New hook asks first only when it replaces a hook or turns deploy on push
// off, and sits in the card that re-renders, so it knows after the first
// hook.
func TestNewHookAsksOnlyToReplace(t *testing.T) {
	hash := "hash"
	for _, test := range []struct {
		app  model.App
		asks string
	}{
		{model.App{Name: "shop"}, ""},
		{model.App{Name: "shop", DeployHookHash: &hash}, "The old URL stops working"},
		{model.App{Name: "shop", Repo: "me/shop"}, ""},
		{model.App{Name: "shop", Repo: "me/shop", AutoDeploy: true}, "Pushes to me/shop no longer deploy shop"},
	} {
		var html strings.Builder
		if err := appCI(RepoView{App: test.app}).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
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
		"docker.io/me/shop:v1": `placeholder="docker.io/me/shop:v1"`,
		"":                     `placeholder="ghcr.io/me/shop:3f2a9c1"`,
	} {
		var html strings.Builder
		app := model.App{Name: "shop", LiveImage: image, DeployHookHash: &hash}
		if err := appCI(RepoView{App: app, HookURL: "https://moin.example.com/webhooks/deploy/dph_x"}).Render(projectPageContext("/app/tiny-rocket-co/apps/shop"), &html); err != nil {
			t.Fatal(err)
		}
		page := html.String()
		if !strings.Contains(page, want) || !strings.Contains(page, "Keep it secret, like a password.") || strings.Contains(page, "GITHUB") {
			t.Errorf("image %q: %s", image, page)
		}
	}
}

// Who brings new versions is one answer: the one the app has now is
// picked, GitHub while it has none, CI while a hook deploys with the
// compose file of the linked repo. The link form picks no repo before you
// do; while the hook is on it only links, without deploy on push.
func TestSourceIsOneAnswer(t *testing.T) {
	hash := "hash"
	current := regexp.MustCompile(`aria-current="page"(?: data-live)? data-source="(\w+)"`)
	repos := []service.GitHubRepoOf{{GitHubRepo: service.GitHubRepo{FullName: "me/shop"}}}
	for _, test := range []struct {
		want string
		app  model.App
	}{
		{"", model.App{Name: "shop"}},
		{"github", model.App{Name: "shop", Repo: "me/shop", Branch: "main"}},
		{"ci", model.App{Name: "shop", DeployHookHash: &hash}},
		{"ci", model.App{Name: "shop", Repo: "me/shop", Branch: "main", DeployHookHash: &hash}},
	} {
		want, app := test.want, test.app
		// The tab in use opens by itself and says so; another tab shows
		// only its own part, without "In use".
		for _, show := range []string{"", "github", "ci"} {
			var html strings.Builder
			if err := AppSource(RepoView{App: app, Enabled: true, Connected: true, Repos: repos, Show: show}).Render(projectPageContext("/app/tiny-rocket-co/apps/shop/deploys"), &html); err != nil {
				t.Fatal(err)
			}
			page := html.String()
			shown := cmp.Or(show, want, "github")
			if found := current.FindAllStringSubmatch(page, -1); len(found) != 1 || found[0][1] != shown {
				t.Errorf("%s, show %q: open tab %v", want, show, found)
			}
			if live := regexp.MustCompile(`data-live data-source="(\w+)"`).FindAllStringSubmatch(page, -1); (want == "" && len(live) != 0) || (want != "" && (len(live) != 1 || live[0][1] != want)) {
				t.Errorf("%s, show %q: in use %v", want, show, live)
			}
			if shown == "github" && app.Repo == "" && !strings.Contains(page, "Pick a repo") {
				t.Errorf("%s: the link form picked a repo: %s", want, page)
			}
			if shown == "github" && app.Repo == "" {
				if onlyLinks := !strings.Contains(page, `name="deploy"`) && !strings.Contains(page, "link-auto-deploy"); onlyLinks != (want == "ci") {
					t.Errorf("%s: link only links %v", want, onlyLinks)
				}
			}
			if shown == "github" && app.Repo != "" {
				if switchShown := strings.Contains(page, "repo-auto-deploy"); switchShown == (want == "ci") {
					t.Errorf("%s: deploy on push switch shown %v", want, switchShown)
				}
			}
			if composeFrom := strings.Contains(page, "data-compose-from"); composeFrom != (shown == "ci" && app.Repo != "") {
				t.Errorf("%s, show %q: compose.yml from the repo shown %v", want, show, composeFrom)
			}
		}
	}
}
