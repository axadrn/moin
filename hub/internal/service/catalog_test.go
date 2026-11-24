package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/axadrn/moin/hub/internal/catalog"
	"github.com/axadrn/moin/protocol"
)

func newCatalogService(t *testing.T) (*CatalogService, *AppService, *fakeDaemon) {
	t.Helper()
	apps, _, _, daemon := newAppService(t)
	entries, err := catalog.Load(os.DirFS(filepath.Join("..", "..", "..", "catalog")))
	if err != nil {
		t.Fatal(err)
	}
	store := catalog.NewStore(entries, catalog.Registry{})
	store.Set("umami", []catalog.Version{{Tag: "3.4.1"}, {Tag: "3.4.0"}, {Tag: "3.3.1"}})
	return NewCatalogService(store, apps), apps, daemon
}

// TestCatalogInstall: an install creates the app, gives it VERSION and a
// fresh secret for every ${NAME} of its compose.yml, and has the daemon
// deploy the app by name, from its own catalog: no bucket, no compose file
// from the hub.
func TestCatalogInstall(t *testing.T) {
	catalogs, apps, daemon := newCatalogService(t)
	app, deploy, login, err := catalogs.Install(context.Background(), owner, CatalogInstall{Slug: "umami", Server: "web-1", Domain: "stats.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if app.Name != "umami" || app.Catalog != "umami" || deploy.Number != 1 || len(login) != 0 {
		t.Errorf("app %s catalog %q, deploy %d, login %v", app.Name, app.Catalog, deploy.Number, login)
	}
	env := daemon.env(t, app.Folder)
	if env["VERSION"] != "3.4.1" || len(env["DB_PASSWORD"]) != 43 || len(env["APP_SECRET"]) != 43 || env["DB_PASSWORD"] == env["APP_SECRET"] {
		t.Errorf("env = %v", env)
	}
	var start protocol.DeployStart
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.Catalog != "umami" || start.SourceURL != "" || start.Domains == nil || len(start.Domains.Domains) != 1 {
		t.Errorf("deploy start = %+v", start)
	}
	listed, _ := apps.Get(owner.WorkspaceID, "umami")
	info, ok := catalogs.Info(listed)
	if !ok || info.Version != "3.4.1" || info.Newer {
		t.Errorf("catalog info = %+v, %v", info, ok)
	}
}

// TestCatalogInstallShowsTheLogin: the secret the first sign-in needs comes
// back once, from the install; the others never do.
func TestCatalogInstallShowsTheLogin(t *testing.T) {
	catalogs, _, daemon := newCatalogService(t)
	app, _, login, err := catalogs.Install(context.Background(), owner, CatalogInstall{Slug: "miniflux", Server: "web-1", Version: "2.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	env := daemon.env(t, app.Folder)
	if len(login) != 1 || login["ADMIN_PASSWORD"] == "" || login["ADMIN_PASSWORD"] != env["ADMIN_PASSWORD"] || env["DB_PASSWORD"] == "" {
		t.Errorf("login = %v", login)
	}
}

func TestCatalogInstallRefuses(t *testing.T) {
	catalogs, apps, _ := newCatalogService(t)
	for name, input := range map[string]CatalogInstall{
		"unknown app":     {Slug: "wordpress", Server: "web-1"},
		"no such version": {Slug: "umami", Server: "web-1", Version: "9.9.9"},
		"bad version":     {Slug: "umami", Server: "web-1", Version: "3.4.1; rm -rf /"},
		"bad name":        {Slug: "umami", Server: "web-1", Name: "My App"},
		"no server":       {Slug: "umami", Server: "nope"},
	} {
		if _, _, _, err := catalogs.Install(context.Background(), owner, input); err == nil {
			t.Errorf("%s: installed", name)
		}
	}
	if list, _ := apps.List(owner.WorkspaceID); len(list) != 0 {
		t.Errorf("a refused install left %d apps", len(list))
	}
}

// TestCatalogUpdate: an update sets VERSION, keeps the secrets, and an app
// of your own refuses it.
func TestCatalogUpdate(t *testing.T) {
	catalogs, apps, daemon := newCatalogService(t)
	app, _, _, err := catalogs.Install(context.Background(), owner, CatalogInstall{Slug: "umami", Server: "web-1", Version: "3.3.1"})
	if err != nil {
		t.Fatal(err)
	}
	before := daemon.env(t, app.Folder)
	listed, _ := apps.Get(owner.WorkspaceID, app.ID)
	if info, _ := catalogs.Info(listed); !info.Newer || info.Latest.Tag != "3.4.1" {
		t.Errorf("before the update = %+v", info)
	}
	finishDeploys(t, apps, app.ID)
	_, deploy, err := catalogs.Update(context.Background(), owner, app.Name, "")
	if err != nil {
		t.Fatal(err)
	}
	after := daemon.env(t, app.Folder)
	if after["VERSION"] != "3.4.1" || after["DB_PASSWORD"] != before["DB_PASSWORD"] || deploy.Number != 2 {
		t.Errorf("after the update: env %v, deploy %d", after, deploy.Number)
	}
	var start protocol.DeployStart
	daemon.last(t, protocol.TypeDeployStart, &start)
	if start.Catalog != "umami" || start.Number != 2 {
		t.Errorf("deploy start = %+v", start)
	}
	own, err := apps.Create(owner, "blog", "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalogs.Update(context.Background(), owner, own.Name, ""); !errors.Is(err, ErrNotCatalog) {
		t.Errorf("update of an own app = %v", err)
	}
}

// finishDeploys marks the app's running deploys done, as the daemon would.
func finishDeploys(t *testing.T, apps *AppService, appID string) {
	t.Helper()
	_, deploys, err := apps.Deploys(owner.WorkspaceID, appID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, deploy := range deploys {
		apps.finish(deploy.ID, protocol.DeployDone{DeployID: deploy.ID})
	}
}
