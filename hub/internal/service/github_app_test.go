package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/secret"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/jmoiron/sqlx"
)

// githubAppOf is a GitHubAppService that has github, for the tests of the
// GitHub calls.
func githubAppOf(github *GitHub) *GitHubAppService {
	service := &GitHubAppService{}
	service.github.Store(github)
	return service
}

var (
	manifestKeyOnce sync.Once
	manifestKey     string
)

// testGitHubKey is a PEM private key, as GitHub hands one out.
func testGitHubKey(t *testing.T) string {
	t.Helper()
	manifestKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		manifestKey = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	})
	return manifestKey
}

type githubAppTest struct {
	database *sqlx.DB
	box      *secret.Box
	service  *GitHubAppService
	admin    *model.User
	member   *model.User
	// conversions counts the codes GitHub traded.
	conversions *[]string
	apiURL      string
}

// newGitHubAppTest is a hub without a GitHub App, whose admin is
// admin@example.com, and a fake GitHub that trades the code "good" for an
// app.
func newGitHubAppTest(t *testing.T, env GitHubAppSettings) githubAppTest {
	t.Helper()
	database := testutil.NewDatabase(t)
	admin := testutil.SeedUser(t, database, testutil.UserOptions{ID: "admin", Email: "admin@example.com", Verified: true}).User
	member := testutil.SeedUser(t, database, testutil.UserOptions{ID: "member", Verified: true}).User
	box, err := secret.New(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	conversions := []string{}
	var mu sync.Mutex
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/app-manifests/"), "/conversions")
		if r.Method != http.MethodPost || !ok || r.Header.Get("Authorization") != "" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		conversions = append(conversions, code)
		mu.Unlock()
		if code != "good" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 4242, "slug": "moin-hub-example-com", "name": "moin-hub-example-com", "client_id": "Iv23.app",
			"client_secret": "client-secret-1", "webhook_secret": "webhook-secret-1", "pem": testGitHubKey(t),
		})
	}))
	t.Cleanup(github.Close)
	service, err := NewGitHubAppService(env, repository.NewGitHubApps(database), box, repository.NewActivity(database), NewHubAdmins([]string{"Admin@example.com"}), "https://hub.example.com/", github.URL, "https://github.example")
	if err != nil {
		t.Fatal(err)
	}
	return githubAppTest{database: database, box: box, service: service, admin: admin, member: member, conversions: &conversions, apiURL: github.URL}
}

// state is the state of the manifest form for user.
func (g githubAppTest) state(t *testing.T, user *model.User) string {
	t.Helper()
	form := g.service.Manifest(user)
	if form == nil {
		t.Fatalf("no manifest for %s", user.Email)
	}
	action, err := url.Parse(form.Action)
	if err != nil {
		t.Fatal(err)
	}
	return action.Query().Get("state")
}

func (g githubAppTest) actor() Actor {
	return Actor{WorkspaceID: "workspace-admin", UserID: g.admin.ID, Source: Source{Via: ViaWeb}}
}

// TestGitHubAppManifestAsksForWhatTheHubUses: the manifest names the
// hub's URLs and exactly the permissions and events moin uses, for the
// admin's own account, private.
func TestGitHubAppManifestAsksForWhatTheHubUses(t *testing.T) {
	g := newGitHubAppTest(t, GitHubAppSettings{})
	form := g.service.Manifest(g.admin)
	if form == nil {
		t.Fatal("the admin gets no manifest")
	}
	if !strings.HasPrefix(form.Action, "https://github.example/settings/apps/new?state=") {
		t.Fatalf("action = %q", form.Action)
	}
	var manifest struct {
		Name           string `json:"name"`
		URL            string `json:"url"`
		HookAttributes struct {
			URL    string `json:"url"`
			Active bool   `json:"active"`
		} `json:"hook_attributes"`
		RedirectURL           string            `json:"redirect_url"`
		CallbackURLs          []string          `json:"callback_urls"`
		Public                *bool             `json:"public"`
		RequestOAuthOnInstall bool              `json:"request_oauth_on_install"`
		SetupOnUpdate         bool              `json:"setup_on_update"`
		DefaultPermissions    map[string]string `json:"default_permissions"`
		DefaultEvents         []string          `json:"default_events"`
	}
	if err := json.Unmarshal([]byte(form.Manifest), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "moin-hub-example-com" || manifest.URL != "https://hub.example.com" {
		t.Errorf("name, url = %q, %q", manifest.Name, manifest.URL)
	}
	if manifest.HookAttributes.URL != "https://hub.example.com/webhooks/github" || !manifest.HookAttributes.Active {
		t.Errorf("hook = %+v", manifest.HookAttributes)
	}
	if manifest.RedirectURL != "https://hub.example.com/app/github/created" {
		t.Errorf("redirect_url = %q", manifest.RedirectURL)
	}
	if !slices.Equal(manifest.CallbackURLs, []string{"https://hub.example.com/app/github/setup", "https://hub.example.com/auth/github/callback"}) {
		t.Errorf("callback_urls = %v, want the setup first, then the sign-in", manifest.CallbackURLs)
	}
	if manifest.Public == nil || *manifest.Public || !manifest.RequestOAuthOnInstall || !manifest.SetupOnUpdate {
		t.Errorf("public, oauth on install, setup on update = %v, %v, %v", manifest.Public, manifest.RequestOAuthOnInstall, manifest.SetupOnUpdate)
	}
	want := map[string]string{"contents": "read", "metadata": "read", "statuses": "write", "email_addresses": "read"}
	if len(manifest.DefaultPermissions) != len(want) {
		t.Errorf("permissions = %v, want %v", manifest.DefaultPermissions, want)
	}
	for name, access := range want {
		if manifest.DefaultPermissions[name] != access {
			t.Errorf("permission %s = %q, want %q", name, manifest.DefaultPermissions[name], access)
		}
	}
	if !slices.Equal(manifest.DefaultEvents, []string{"push"}) {
		t.Errorf("events = %v, want push: installation events come by themselves", manifest.DefaultEvents)
	}
}

func TestGitHubAppName(t *testing.T) {
	for appURL, want := range map[string]string{
		"https://moin.example.com":                    "moin-moin-example-com",
		"http://localhost:8090":                       "moin-localhost",
		"https://a-very-long-subdomain.example.co.uk": "moin-a-very-long-subdomain-example",
		"":                                     "moin",
		"https://hub.example.com.":             "moin-hub-example-com",
		"https://abcdefghijklmnopqrstuvwx.com": "moin-abcdefghijklmnopqrstuvwx-com",
	} {
		if got := githubAppName(appURL); got != want || len(got) > 34 {
			t.Errorf("githubAppName(%q) = %q, want %q", appURL, got, want)
		}
	}
}

// TestGitHubAppOnlyForAnAdminWithoutOne: the form is only for a verified
// address of ADMIN_EMAILS, and only while the hub has no app.
func TestGitHubAppOnlyForAnAdminWithoutOne(t *testing.T) {
	g := newGitHubAppTest(t, GitHubAppSettings{})
	unverified := *g.admin
	unverified.EmailVerifiedAt = nil
	for name, user := range map[string]*model.User{"member": g.member, "unverified admin": &unverified, "nobody": nil} {
		if g.service.Manifest(user) != nil {
			t.Errorf("%s gets the manifest", name)
		}
	}
	state := g.state(t, g.admin)
	if err := g.service.Create(context.Background(), g.actor(), g.member, "good", state); !errors.Is(err, ErrGitHubAppNotAllowed) {
		t.Fatalf("a member creates: err = %v, want ErrGitHubAppNotAllowed", err)
	}
	if len(*g.conversions) != 0 {
		t.Fatalf("GitHub was asked for %v", *g.conversions)
	}
	if err := g.service.Create(context.Background(), g.actor(), g.admin, "good", state); err != nil {
		t.Fatal(err)
	}
	if g.service.Manifest(g.admin) != nil {
		t.Fatal("the hub has its app and still offers a new one")
	}
	if err := g.service.Create(context.Background(), g.actor(), g.admin, "good", state); !errors.Is(err, ErrGitHubAppExists) {
		t.Fatalf("a second app: err = %v, want ErrGitHubAppExists", err)
	}
	if len(*g.conversions) != 1 {
		t.Fatalf("GitHub traded %v, want one code", *g.conversions)
	}
}

// TestGitHubAppStateBelongsToTheAdmin: GitHub's return link works only
// with the state of this admin's form, within the hour, and with a code
// that fits in a path.
func TestGitHubAppStateBelongsToTheAdmin(t *testing.T) {
	g := newGitHubAppTest(t, GitHubAppSettings{})
	other := testutil.SeedUser(t, g.database, testutil.UserOptions{ID: "second-admin", Email: "second@example.com", Verified: true}).User
	g.service.admins = NewHubAdmins([]string{"admin@example.com", "second@example.com"})
	ctx := context.Background()
	for name, state := range map[string]string{
		"empty":         "",
		"made up":       "not-a-state",
		"another admin": g.state(t, other),
		"another place": g.box.Seal(g.admin.ID+"|9999999999", "github-state"),
	} {
		if err := g.service.Create(ctx, g.actor(), g.admin, "good", state); !errors.Is(err, ErrGitHubAppState) {
			t.Errorf("%s state: err = %v, want ErrGitHubAppState", name, err)
		}
	}
	state := g.state(t, g.admin)
	for _, code := range []string{"", "../../user", "good/conversions?x="} {
		if err := g.service.Create(ctx, g.actor(), g.admin, code, state); !errors.Is(err, ErrGitHubAppState) {
			t.Errorf("code %q: err = %v, want ErrGitHubAppState", code, err)
		}
	}
	g.service.now = func() time.Time { return time.Now().Add(githubAppStateExpiry + time.Minute) }
	if err := g.service.Create(ctx, g.actor(), g.admin, "good", state); !errors.Is(err, ErrGitHubAppState) {
		t.Errorf("an old state: err = %v, want ErrGitHubAppState", err)
	}
	g.service.now = time.Now
	if err := g.service.Create(ctx, g.actor(), g.admin, "used", state); err == nil {
		t.Error("a code GitHub does not know creates an app")
	}
	if len(*g.conversions) != 1 || g.service.GitHub() != nil {
		t.Fatalf("conversions = %v, app = %v: want one try and no app", *g.conversions, g.service.GitHub())
	}
}

// TestGitHubAppCreatedFromTheManifest: the code becomes the hub's app at
// once, kept in the database with its secrets sealed, and a hub that
// starts again has it.
func TestGitHubAppCreatedFromTheManifest(t *testing.T) {
	g := newGitHubAppTest(t, GitHubAppSettings{})
	if g.service.GitHub() != nil || g.service.SignsIn() {
		t.Fatal("a new hub has a GitHub App")
	}
	if err := g.service.Create(context.Background(), g.actor(), g.admin, "good", g.state(t, g.admin)); err != nil {
		t.Fatal(err)
	}
	github := g.service.GitHub()
	if github == nil || !g.service.SignsIn() || github.appID != "4242" || github.slug != "moin-hub-example-com" || github.clientID != "Iv23.app" || github.webhookSecret != "webhook-secret-1" {
		t.Fatalf("app = %+v, signs in %v", github, g.service.SignsIn())
	}
	if !strings.HasPrefix(github.InstallURL("s"), "https://github.example/apps/moin-hub-example-com/installations/new") {
		t.Fatalf("install URL = %q", github.InstallURL("s"))
	}

	var count int
	if err := g.database.Get(&count, `SELECT COUNT(*) FROM github_app`); err != nil || count != 1 {
		t.Fatalf("rows = %d, %v", count, err)
	}
	row, err := repository.NewGitHubApps(g.database).Get()
	if err != nil || row == nil || row.AppID != "4242" || row.ClientID != "Iv23.app" || row.CreatedBy == nil || *row.CreatedBy != "admin" {
		t.Fatalf("row = %+v, %v", row, err)
	}
	for column, stored := range map[string][2]string{
		"client_secret":  {row.ClientSecret, "client-secret-1"},
		"webhook_secret": {row.WebhookSecret, "webhook-secret-1"},
		"private_key":    {row.PrivateKey, testGitHubKey(t)},
	} {
		sealed, plain := stored[0], stored[1]
		if sealed == "" || strings.Contains(sealed, plain) || strings.Contains(sealed, "PRIVATE KEY") {
			t.Errorf("%s is stored as %q, want it sealed", column, sealed)
		}
		if opened, err := g.box.Open(sealed, "github_app/"+column); err != nil || opened != plain {
			t.Errorf("%s opens to %q, %v", column, opened, err)
		}
	}

	again, err := NewGitHubAppService(GitHubAppSettings{}, repository.NewGitHubApps(g.database), g.box, nil, NewHubAdmins([]string{"admin@example.com"}), "https://hub.example.com", g.apiURL, "https://github.example")
	if err != nil {
		t.Fatal(err)
	}
	if loaded := again.GitHub(); loaded == nil || loaded.appID != "4242" || loaded.clientSecret != "client-secret-1" || !loaded.key.Equal(github.key) {
		t.Fatalf("after a restart the app is %+v", loaded)
	}
	if again.Manifest(g.admin) != nil {
		t.Fatal("after a restart the hub offers a second app")
	}

	other, err := secret.New(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewGitHubAppService(GitHubAppSettings{}, repository.NewGitHubApps(g.database), other, nil, nil, "https://hub.example.com", g.apiURL, ""); err == nil || !strings.Contains(err.Error(), "SECRETS_KEY") {
		t.Fatalf("another SECRETS_KEY: err = %v, want it to name SECRETS_KEY", err)
	}

	events, err := repository.NewActivity(g.database).ListByWorkspace("workspace-admin", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[0].Action != "hub.github_app_created" || events[0].TargetID != "moin-hub-example-com" || strings.Contains(events[0].Metadata, "secret") {
		t.Fatalf("activity = %+v", events)
	}
}

// TestGitHubAppEnvWins: GITHUB_APP_* wins over the app in the database,
// and a hub with it offers no new one.
func TestGitHubAppEnvWins(t *testing.T) {
	g := newGitHubAppTest(t, GitHubAppSettings{})
	if err := g.service.Create(context.Background(), g.actor(), g.admin, "good", g.state(t, g.admin)); err != nil {
		t.Fatal(err)
	}
	env := GitHubAppSettings{ID: "7", Slug: "moin-env", PrivateKey: testGitHubKey(t), WebhookSecret: "env-hook", ClientID: "Iv1.env", ClientSecret: "env-secret"}
	service, err := NewGitHubAppService(env, repository.NewGitHubApps(g.database), g.box, nil, NewHubAdmins([]string{"admin@example.com"}), "https://hub.example.com", g.apiURL, "")
	if err != nil {
		t.Fatal(err)
	}
	if github := service.GitHub(); github == nil || github.appID != "7" || github.slug != "moin-env" || github.clientSecret != "env-secret" {
		t.Fatalf("app = %+v, want the one of the env", github)
	}

	fresh := newGitHubAppTest(t, env)
	if fresh.service.Manifest(fresh.admin) != nil {
		t.Fatal("a hub with GITHUB_APP_* offers a new app")
	}
	if err := fresh.service.Create(context.Background(), fresh.actor(), fresh.admin, "good", fresh.box.Seal("admin|9999999999", "github-app-state")); !errors.Is(err, ErrGitHubAppExists) {
		t.Fatalf("create next to the env: err = %v, want ErrGitHubAppExists", err)
	}
	var stored int
	if err := fresh.database.Get(&stored, `SELECT COUNT(*) FROM github_app`); err != nil || stored != 0 {
		t.Fatalf("stored = %d, %v", stored, err)
	}

	partial, err := NewGitHubAppService(GitHubAppSettings{ID: "7", PrivateKey: testGitHubKey(t)}, nil, nil, nil, nil, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if partial.GitHub() == nil || partial.SignsIn() {
		t.Fatal("an app without its client ID and secret signs people in")
	}
	var none *GitHubAppService
	if none.GitHub() != nil || none.SignsIn() || none.Manifest(g.admin) != nil {
		t.Fatal("no service has an app")
	}
}
