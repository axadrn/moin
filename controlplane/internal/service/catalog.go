package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/catalog"
	"github.com/axadrn/moin/controlplane/internal/model"
)

var (
	ErrCatalogApp     = errors.New("the catalog has no such app")
	ErrCatalogVersion = errors.New("this version is not a release of the app: pick one from its list")
	ErrNotCatalog     = errors.New("this app was not installed from the catalog")
	ErrCatalogDomain  = errors.New("this app needs a domain: its links and sign-in point to it")
	// versionPattern is what an image tag may be.
	versionPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
)

// CatalogService installs the apps of the catalog and updates them. An
// install is an ordinary app: its compose.yml from the catalog goes to
// the server like a deploy from the CLI, its secrets are env vars, and
// VERSION picks the image tag.
type CatalogService struct {
	store *catalog.Store
	apps  *AppService
}

func NewCatalogService(store *catalog.Store, apps *AppService) *CatalogService {
	service := &CatalogService{store: store, apps: apps}
	store.OnRefresh = service.CheckUpdates
	return service
}

// Run reads the releases of the catalog until ctx ends; each new one
// checks the installed apps for an update.
func (s *CatalogService) Run(ctx context.Context) {
	s.store.Run(ctx)
}

// CheckUpdates records update.available for every app installed from slug
// that runs an older release than the newest, once per release: the alert
// goes out with it. The release it went out for stays with the app, so a
// restart never sends it twice.
func (s *CatalogService) CheckUpdates(slug string) {
	entry, ok := s.store.Entry(slug)
	if !ok {
		return
	}
	latest, ok := s.store.Latest(slug)
	if !ok {
		return
	}
	apps, err := s.apps.apps.CatalogApps(slug)
	if err != nil {
		slog.Error("catalog: list installed apps failed", "app", slug, "error", err)
		return
	}
	for _, app := range apps {
		if app.CatalogNotified == latest.Tag {
			continue
		}
		env, err := s.apps.Env(app.WorkspaceID, app.ID)
		if err != nil {
			continue
		}
		current := env[catalog.VersionVar]
		if current == "" || catalog.Compare(latest.Tag, current) <= 0 {
			continue
		}
		if err := s.apps.apps.SetCatalogNotified(app.ID, latest.Tag); err != nil {
			slog.Error("catalog: store the notified release failed", "app_id", app.ID, "error", err)
			continue
		}
		s.apps.servers.record(System(app.WorkspaceID), "update.available", "app", app.Name, map[string]string{"catalog": entry.Name, "version": latest.Tag, "current": current})
	}
}

// Store is the catalog: its entries and their releases.
func (s *CatalogService) Store() *catalog.Store { return s.store }

// CatalogInstall is what an install needs. Version, Name and Domain may
// be empty: the newest release, the slug, no domain.
type CatalogInstall struct {
	Slug, Version, Server, Name, Domain string
}

// CatalogApp is what the catalog knows about an installed app: its entry,
// the version it runs and the newest release.
type CatalogApp struct {
	Entry   catalog.Entry
	Version string
	Latest  catalog.Version
	// Newer is set when Latest is newer than Version.
	Newer bool
}

// Install creates the app on a server, gives it its secrets and VERSION,
// and deploys it.
func (s *CatalogService) Install(ctx context.Context, actor Actor, input CatalogInstall) (*model.App, *model.Deploy, error) {
	entry, ok := s.store.Entry(strings.TrimSpace(input.Slug))
	if !ok {
		return nil, nil, ErrCatalogApp
	}
	version, err := s.version(entry, input.Version)
	if err != nil {
		return nil, nil, err
	}
	domain := strings.ToLower(strings.TrimSpace(input.Domain))
	if entry.NeedsURL() && domain == "" {
		return nil, nil, ErrCatalogDomain
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = entry.Slug
	}
	if !appNamePattern.MatchString(name) {
		return nil, nil, ErrAppName
	}
	// Check what would stop the deploy before the app exists.
	server, err := s.apps.servers.servers.ByIDOrNameForWorkspace(strings.TrimSpace(input.Server), actor.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	if !s.apps.servers.Online(server.ID) {
		return nil, nil, ErrServerOffline
	}
	if _, err := s.apps.apps.DefaultBucket(actor.WorkspaceID); err != nil {
		return nil, nil, ErrUploadBucket
	}
	app, err := s.apps.Create(actor, name, server.ID)
	if err != nil {
		return nil, nil, err
	}
	err = s.apps.apps.SetCatalog(app.ID, entry.Slug)
	if err != nil {
		return app, nil, err
	}
	app.Catalog = entry.Slug
	env := map[string]string{catalog.VersionVar: version}
	for _, name := range entry.Secrets {
		env[name] = newSecret(name)
	}
	if entry.NeedsURL() {
		env[catalog.URLVar] = "https://" + domain
		env[catalog.DomainVar] = domain
	}
	_, err = s.apps.SetEnv(actor, app.ID, env, nil)
	if err != nil {
		return app, nil, err
	}
	if domain != "" {
		_, err = s.apps.SetDomains(actor, app.ID, "", []string{domain}, "")
		if err != nil {
			return app, nil, err
		}
	}
	deploy, err := s.deploy(ctx, actor, "app.installed", app, entry, version)
	return app, deploy, err
}

// Update sets the app to version, the newest release when empty, and
// deploys it. moin remembers the moment before, so a restore can go back
// to it. An older version goes too, but migrations may not go back.
func (s *CatalogService) Update(ctx context.Context, actor Actor, idOrName, version string) (*model.App, *model.Deploy, error) {
	app, err := s.apps.Get(actor.WorkspaceID, idOrName)
	if err != nil {
		return nil, nil, err
	}
	if app.Catalog == "" {
		return app, nil, ErrNotCatalog
	}
	entry, ok := s.store.Entry(app.Catalog)
	if !ok {
		return app, nil, ErrCatalogApp
	}
	version, err = s.version(entry, version)
	if err != nil {
		return app, nil, err
	}
	current, err := s.apps.Env(actor.WorkspaceID, app.ID)
	if err != nil {
		return app, nil, err
	}
	env := map[string]string{catalog.VersionVar: version}
	// A newer compose.yml may ask for a secret the app does not have yet.
	for _, name := range entry.Secrets {
		if current[name] == "" {
			env[name] = newSecret(name)
		}
	}
	if current[catalog.VersionVar] != version || len(env) > 1 {
		_, err = s.apps.SetEnv(actor, app.ID, env, nil)
		if err != nil {
			return app, nil, err
		}
	}
	deploy, err := s.deploy(ctx, actor, "app.updated", app, entry, version)
	if err == nil {
		s.apps.markUpdate(deploy.ID, version)
	}
	return app, deploy, err
}

// deploy sends the entry's compose.yml to the app's server and records
// action with the version.
func (s *CatalogService) deploy(ctx context.Context, actor Actor, action string, app *model.App, entry catalog.Entry, version string) (*model.Deploy, error) {
	deploy, err := s.apps.DeployFiles(ctx, actor, "", app.ID, map[string][]byte{"compose.yml": entry.Compose})
	if err != nil {
		return nil, err
	}
	s.apps.servers.record(actor, action, "app", app.Name, map[string]string{"catalog": entry.Name, "version": version, "number": strconv.Itoa(deploy.Number)})
	return deploy, nil
}

// version is the release to run: wanted, or the newest when empty. Before
// the registry ever answered, any tag the entry's filter takes goes.
func (s *CatalogService) version(entry catalog.Entry, wanted string) (string, error) {
	wanted = strings.TrimSpace(wanted)
	releases := s.store.Versions(entry.Slug)
	if wanted == "" {
		if len(releases) == 0 {
			return "", ErrCatalogVersion
		}
		return releases[0].Tag, nil
	}
	if !versionPattern.MatchString(wanted) {
		return "", ErrCatalogVersion
	}
	if len(releases) == 0 {
		if !entry.Filter.MatchString(wanted) {
			return "", ErrCatalogVersion
		}
		return wanted, nil
	}
	if !slices.ContainsFunc(releases, func(release catalog.Version) bool { return release.Tag == wanted }) {
		return "", ErrCatalogVersion
	}
	return wanted, nil
}

// Info says what the catalog knows of app; ok is false for an app of your
// own.
func (s *CatalogService) Info(workspaceID string, app *model.App) (CatalogApp, bool) {
	if app.Catalog == "" {
		return CatalogApp{}, false
	}
	entry, ok := s.store.Entry(app.Catalog)
	if !ok {
		return CatalogApp{}, false
	}
	info := CatalogApp{Entry: entry}
	env, err := s.apps.Env(workspaceID, app.ID)
	if err == nil {
		info.Version = env[catalog.VersionVar]
	}
	if latest, ok := s.store.Latest(entry.Slug); ok {
		info.Latest = latest
		info.Newer = info.Version != "" && catalog.Compare(latest.Tag, info.Version) > 0
	}
	return info, true
}

// newSecret is 32 random bytes as URL-safe text: fine in a database URL,
// a header or YAML. Some apps want a key in hex: a name that ends in _HEX
// gets 32 bytes as 64 hex digits, one that ends in _HEX16 16 bytes as 32.
func newSecret(name string) string {
	size, encode := 32, base64.RawURLEncoding.EncodeToString
	switch {
	case strings.HasSuffix(name, "_HEX16"):
		size, encode = 16, hex.EncodeToString
	case strings.HasSuffix(name, "_HEX"):
		encode = hex.EncodeToString
	}
	buffer := make([]byte, size)
	_, _ = rand.Read(buffer)
	return encode(buffer)
}
