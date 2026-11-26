package service

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/secret"
	"github.com/axadrn/moin/internal/build"
)

// GitHubAppSettings is a GitHub App as moin uses it.
type GitHubAppSettings struct {
	ID            string
	Slug          string
	PrivateKey    string
	WebhookSecret string
	ClientID      string
	ClientSecret  string
}

var (
	ErrGitHubAppNotAllowed = errors.New("only an admin of this hub creates its GitHub App")
	ErrGitHubAppExists     = errors.New("this hub has its GitHub App already")
	ErrGitHubAppState      = errors.New("the link from GitHub expired or belongs to another account: create the GitHub App again")
	ErrGitHubAppAnswer     = errors.New("GitHub did not hand over the new app")
	// githubAppCodePattern is the code GitHub sends back, checked before it
	// goes into a path.
	githubAppCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
)

// githubAppStateExpiry is how long GitHub gives the manifest flow.
const githubAppStateExpiry = time.Hour

// GitHubAppService is the GitHub App of this hub: the one of GITHUB_APP_*
// when the env names one, else the one an admin created with one click
// from a manifest, kept in the database. Once the hub has it, it stays.
type GitHubAppService struct {
	apps     *repository.GitHubApps
	box      *secret.Box
	activity *repository.Activity
	admins   HubAdmins
	appURL   string
	apiURL   string
	webURL   string
	http     *http.Client
	now      func() time.Time

	// mu keeps two creates apart.
	mu     sync.Mutex
	github atomic.Pointer[GitHub]
}

// NewGitHubAppService loads the app of env, or else the one in apps.
// Without apps the hub knows only env.
func NewGitHubAppService(env GitHubAppSettings, apps *repository.GitHubApps, box *secret.Box, activity *repository.Activity, admins HubAdmins, appURL, apiURL, webURL string) (*GitHubAppService, error) {
	s := &GitHubAppService{
		apps: apps, box: box, activity: activity, admins: admins,
		appURL: strings.TrimRight(appURL, "/"),
		apiURL: strings.TrimRight(cmp.Or(apiURL, "https://api.github.com"), "/"),
		webURL: strings.TrimRight(cmp.Or(webURL, "https://github.com"), "/"),
		http:   &http.Client{Timeout: 30 * time.Second}, now: time.Now,
	}
	settings, err := s.settings(env)
	if err != nil {
		return nil, err
	}
	github, err := NewGitHub(settings.ID, settings.Slug, settings.PrivateKey, settings.WebhookSecret, settings.ClientID, settings.ClientSecret, s.apiURL, s.webURL)
	if err != nil && env.ID != "" {
		return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("the GitHub App in the database: %w", err)
	}
	s.github.Store(github)
	return s, nil
}

// settings answers which GitHub App the hub uses: env when it names one,
// else the one in the database, else none.
func (s *GitHubAppService) settings(env GitHubAppSettings) (GitHubAppSettings, error) {
	if s.apps == nil {
		return env, nil
	}
	stored, err := s.apps.Get()
	if err != nil {
		return GitHubAppSettings{}, err
	}
	if env.ID != "" {
		if stored != nil {
			slog.Info("GITHUB_APP_ID is set: the hub uses that GitHub App, not the one created with one click", "github_app", stored.Slug)
		}
		return env, nil
	}
	if stored == nil {
		return GitHubAppSettings{}, nil
	}
	settings := GitHubAppSettings{ID: stored.AppID, Slug: stored.Slug, ClientID: stored.ClientID}
	for _, field := range []struct {
		name   string
		sealed string
		value  *string
	}{
		{"client_secret", stored.ClientSecret, &settings.ClientSecret},
		{"webhook_secret", stored.WebhookSecret, &settings.WebhookSecret},
		{"private_key", stored.PrivateKey, &settings.PrivateKey},
	} {
		*field.value, err = s.box.Open(field.sealed, "github_app/"+field.name)
		if err != nil {
			return GitHubAppSettings{}, fmt.Errorf("open the GitHub App in the database: put back the SECRETS_KEY it was made with, or set GITHUB_APP_*: %w", err)
		}
	}
	return settings, nil
}

// GitHub is the hub's GitHub App, nil without one.
func (s *GitHubAppService) GitHub() *GitHub {
	if s == nil {
		return nil
	}
	return s.github.Load()
}

// SignsIn says whether people sign in with GitHub: the app is there with
// its client ID and secret.
func (s *GitHubAppService) SignsIn() bool {
	github := s.GitHub()
	return github != nil && github.ChecksInstaller()
}

// Manifest is the form that creates the hub's GitHub App on the personal
// account of user, for an admin of the hub while it has none; else nil.
func (s *GitHubAppService) Manifest(user *model.User) *model.GitHubAppManifest {
	if s == nil || s.apps == nil || s.GitHub() != nil || !s.admins.Has(user) {
		return nil
	}
	manifest, _ := json.Marshal(map[string]any{
		"name":            githubAppName(s.appURL),
		"url":             s.appURL,
		"hook_attributes": map[string]any{"url": s.appURL + "/webhooks/github", "active": true},
		"redirect_url":    s.appURL + "/app/github/created",
		// GitHub sends an install back to the first, a sign-in to the
		// second.
		"callback_urls": []string{s.appURL + "/app/github/setup", s.appURL + "/auth/github/callback"},
		// Public, so teammates on other GitHub accounts sign in and
		// install it; the hub checks who installed it.
		"public":                   true,
		"request_oauth_on_install": true,
		"setup_on_update":          true,
		"default_permissions": map[string]string{
			"contents":        "read",
			"metadata":        "read",
			"statuses":        "write",
			"email_addresses": "read",
		},
		// GitHub sends installation events to every app by itself.
		"default_events": []string{"push"},
	})
	state := s.box.Seal(user.ID+"|"+strconv.FormatInt(s.now().Add(githubAppStateExpiry).Unix(), 10), "github-app-state")
	return &model.GitHubAppManifest{
		Action:   s.webURL + "/settings/apps/new?state=" + url.QueryEscape(state),
		Manifest: string(manifest),
	}
}

// githubAppName is the name the app gets on GitHub, which must be unique
// there and at most 34 characters: moin and the host of the hub, like
// moin-moin-example-com. The admin may change it on GitHub.
func githubAppName(appURL string) string {
	name := build.Name
	if parsed, err := url.Parse(appURL); err == nil && parsed.Hostname() != "" {
		name += "-" + strings.ReplaceAll(parsed.Hostname(), ".", "-")
	}
	return strings.TrimRight(name[:min(len(name), 34)], "-")
}

// Create takes the admin back from GitHub with the code of the new app:
// it trades the code for the app's ID, key and secrets, keeps them in the
// database, the secrets sealed, and the hub uses the app at once.
func (s *GitHubAppService) Create(ctx context.Context, actor Actor, user *model.User, code, state string) error {
	if s.apps == nil || !s.admins.Has(user) {
		return ErrGitHubAppNotAllowed
	}
	opened, err := s.box.Open(state, "github-app-state")
	parts := strings.Split(opened, "|")
	if err != nil || len(parts) != 2 || parts[0] != user.ID {
		return ErrGitHubAppState
	}
	deadline, _ := strconv.ParseInt(parts[1], 10, 64)
	if s.now().Unix() > deadline || !githubAppCodePattern.MatchString(code) {
		return ErrGitHubAppState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.GitHub() != nil {
		return ErrGitHubAppExists
	}
	var answer struct {
		ID            int64   `json:"id"`
		Slug          string  `json:"slug"`
		ClientID      string  `json:"client_id"`
		ClientSecret  string  `json:"client_secret"`
		WebhookSecret *string `json:"webhook_secret"`
		PEM           string  `json:"pem"`
	}
	err = sendGitHub(ctx, s.http, http.MethodPost, s.apiURL+"/app-manifests/"+code+"/conversions", "", nil, &answer)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrGitHubAppAnswer, err)
	}
	if answer.ID == 0 || answer.Slug == "" || answer.ClientID == "" || answer.ClientSecret == "" || answer.WebhookSecret == nil || *answer.WebhookSecret == "" {
		return fmt.Errorf("%w: its ID, slug, client or webhook secret is missing", ErrGitHubAppAnswer)
	}
	settings := GitHubAppSettings{ID: strconv.FormatInt(answer.ID, 10), Slug: answer.Slug, PrivateKey: answer.PEM, WebhookSecret: *answer.WebhookSecret, ClientID: answer.ClientID, ClientSecret: answer.ClientSecret}
	github, err := NewGitHub(settings.ID, settings.Slug, settings.PrivateKey, settings.WebhookSecret, settings.ClientID, settings.ClientSecret, s.apiURL, s.webURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrGitHubAppAnswer, err)
	}
	now := s.now().UTC()
	err = s.apps.Create(&model.GitHubApp{
		AppID:         settings.ID,
		Slug:          settings.Slug,
		ClientID:      settings.ClientID,
		ClientSecret:  s.box.Seal(settings.ClientSecret, "github_app/client_secret"),
		WebhookSecret: s.box.Seal(settings.WebhookSecret, "github_app/webhook_secret"),
		PrivateKey:    s.box.Seal(settings.PrivateKey, "github_app/private_key"),
		CreatedBy:     &user.ID,
		CreatedAt:     now,
	})
	if errors.Is(err, repository.ErrGitHubAppExists) {
		return ErrGitHubAppExists
	}
	if err != nil {
		return err
	}
	s.github.Store(github)
	recordActivity(s.activity, now, actor, "hub.github_app_created", "hub", settings.Slug, nil)
	return nil
}
