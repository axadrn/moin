package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A resolved compose file of the old name moves to the new project: its
// name, networks, named volumes and router labels. Images, a volume the
// file names itself, env and other labels stay. A second run changes
// nothing.
func TestRenameCompose(t *testing.T) {
	body := `{
  "name": "ops-shop",
  "networks": {"default": {"name": "ops-shop_default"}, "ops-proxy": {"name": "ops-proxy", "external": true}},
  "volumes": {"pgdata": {"name": "ops-shop_pgdata"}, "keep": {"name": "my-own-volume"}},
  "services": {
    "web": {
      "image": "ops-shop-web:3",
      "environment": {"NOTE": "ops-shop stays in values"},
      "networks": {"default": null, "ops-proxy": null},
      "labels": {
        "traefik.enable": "true",
        "traefik.docker.network": "ops-proxy",
        "traefik.http.routers.ops-shop.rule": "Host(` + "`shop.com`" + `)",
        "traefik.http.routers.ops-shop.middlewares": "ops-shop-canonical,ops-shop-auth",
        "traefik.http.routers.ops-shop.service": "ops-shop",
        "traefik.http.middlewares.ops-shop-auth.basicauth.users": "ops:$$2y$$05$$abc",
        "traefik.http.services.ops-shop.loadbalancer.server.port": "3000",
        "com.example.mine": "ops-shop"
      }
    },
    "api": {"labels": {"traefik.http.routers.ops-shop-svc-api.service": "ops-shop-svc-api"}}
  }
}`
	var config map[string]any
	if err := json.Unmarshal([]byte(body), &config); err != nil {
		t.Fatal(err)
	}
	if !renameCompose(config, "shop") {
		t.Fatal("nothing changed")
	}
	out, _ := json.Marshal(config)
	got := string(out)
	for _, want := range []string{
		`"name":"moin-shop"`,
		`"default":{"name":"moin-shop_default"}`,
		`"moin-proxy":{"external":true,"name":"moin-proxy"}`,
		`"pgdata":{"name":"moin-shop_pgdata"}`,
		`"keep":{"name":"my-own-volume"}`,
		`"image":"ops-shop-web:3"`,
		`"NOTE":"ops-shop stays in values"`,
		`"traefik.docker.network":"moin-proxy"`,
		`"traefik.http.routers.moin-shop.middlewares":"moin-shop-canonical,moin-shop-auth"`,
		`"traefik.http.routers.moin-shop.service":"moin-shop"`,
		`"traefik.http.middlewares.moin-shop-auth.basicauth.users":"ops:$$2y$$05$$abc"`,
		`"traefik.http.services.moin-shop.loadbalancer.server.port":"3000"`,
		`"traefik.http.routers.moin-shop-svc-api.service":"moin-shop-svc-api"`,
		`"com.example.mine":"ops-shop"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, `"ops-proxy"`) || strings.Contains(got, "routers.ops-shop") {
		t.Errorf("old names left:\n%s", got)
	}
	if renameCompose(config, "shop") {
		t.Error("a second run changed something")
	}
}

// The paths in the agent's own JSON files move with the state; an app's
// source and data are never touched.
func TestRewritePaths(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "shop", "releases", "3")
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(release, "compose.json"), `{"source":"/var/lib/ops/apps/shop/volumes/data"}`)
	write(filepath.Join(release, "detected.json"), `{"host_path":"/var/lib/ops/apps/shop/volumes/data/app.db"}`)
	write(filepath.Join(release, "src", "package.json"), `{"dir":"/var/lib/ops/apps/"}`)
	write(filepath.Join(dir, "shop", "volumes", "data", "config.json"), `{"dir":"/var/lib/ops/apps/"}`)
	if err := rewritePaths(dir, "/var/lib/ops/", "/var/lib/moin/"); err != nil {
		t.Fatal(err)
	}
	read := func(path string) string { body, _ := os.ReadFile(path); return string(body) }
	if got := read(filepath.Join(release, "compose.json")); got != `{"source":"/var/lib/moin/apps/shop/volumes/data"}` {
		t.Errorf("compose.json = %s", got)
	}
	if got := read(filepath.Join(release, "detected.json")); !strings.Contains(got, "/var/lib/moin/apps/shop") {
		t.Errorf("detected.json = %s", got)
	}
	for _, untouched := range []string{filepath.Join(release, "src", "package.json"), filepath.Join(dir, "shop", "volumes", "data", "config.json")} {
		if got := read(untouched); !strings.Contains(got, "/var/lib/ops/") {
			t.Errorf("%s changed: %s", untouched, got)
		}
	}
}

// The state moves in one rename and leaves a link. A crash between the
// two is picked up; a second run does nothing; two agents never merge.
func TestMoveStateDir(t *testing.T) {
	root := t.TempDir()
	from, to := filepath.Join(root, "ops"), filepath.Join(root, "moin")
	if err := os.MkdirAll(filepath.Join(from, "apps", "shop"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, stateFile), []byte(`{"url":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveStateDir(from, to); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(to, "apps", "shop")); err != nil {
		t.Fatalf("not moved: %v", err)
	}
	if target, err := os.Readlink(from); err != nil || target != to {
		t.Fatalf("link = %q, %v", target, err)
	}
	if err := moveStateDir(from, to); err != nil {
		t.Fatalf("second run: %v", err)
	}
	// A crash after the rename, before the link.
	if err := os.Remove(from); err != nil {
		t.Fatal(err)
	}
	if err := moveStateDir(from, to); err != nil {
		t.Fatal(err)
	}
	if target, _ := os.Readlink(from); target != to {
		t.Fatalf("link not made again: %q", target)
	}
	// Two agents: nothing moves.
	other := filepath.Join(root, "other")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, stateFile), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveStateDir(other, to); err == nil {
		t.Fatal("moved over an agent")
	}
	if _, err := os.Stat(filepath.Join(other, stateFile)); err != nil {
		t.Fatal("the other agent's state is gone")
	}
}

// Compose projects of both names belong to their app, for metrics while
// an app has not moved.
func TestAppOfProject(t *testing.T) {
	for project, want := range map[string]string{"moin-shop": "shop", "ops-shop": "shop", "other": ""} {
		got, _ := appOfProject(project)
		if got != want {
			t.Errorf("appOfProject(%q) = %q, want %q", project, got, want)
		}
	}
	legacyApps.Store("blog", true)
	defer legacyApps.Delete("blog")
	if project("blog") != "ops-blog" || project("shop") != "moin-shop" {
		t.Errorf("project = %s, %s", project("blog"), project("shop"))
	}
}

// x-moin wins over x-ops; x-ops alone still works while the old name does.
func TestConfigBlock(t *testing.T) {
	block, key, err := configBlock(map[string]any{"x-ops": map[string]any{"app": "old"}, "x-moin": map[string]any{"app": "new"}})
	if err != nil || key != "x-moin" || block.(map[string]any)["app"] != "new" {
		t.Errorf("both: %v %s %v", block, key, err)
	}
	block, key, err = configBlock(map[string]any{"x-ops": map[string]any{"app": "old"}})
	if err != nil || key != "x-ops" || block.(map[string]any)["app"] != "old" {
		t.Errorf("old: %v %s %v", block, key, err)
	}
	if block, _, err := configBlock(map[string]any{}); block != nil || err != nil {
		t.Errorf("none: %v %v", block, err)
	}
}

// Volume names follow the project.
func TestNewVolumeName(t *testing.T) {
	if got := newVolumeName("shop", "ops-shop_pgdata"); got != "moin-shop_pgdata" {
		t.Errorf("newVolumeName = %s", got)
	}
}

// The migration state survives a restart.
func TestRenameState(t *testing.T) {
	dir := t.TempDir()
	m := newRename(dir, newApps(dir, nil), nil)
	m.state.Apps["shop"] = "done"
	m.state.Volumes = append(m.state.Volumes, "ops-shop_pgdata")
	m.save()
	again := newRename(dir, newApps(dir, nil), nil)
	if again.state.Apps["shop"] != "done" || len(again.state.Volumes) != 1 {
		t.Errorf("state = %+v", again.state)
	}
}
