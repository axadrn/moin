package daemon

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/axadrn/moin/catalog"
	"github.com/axadrn/moin/protocol"
)

// TestMakeSecrets: a catalog deploy makes a value for each secret its
// compose file needs that the app lacks, and keeps the ones it has.
func TestMakeSecrets(t *testing.T) {
	a := newApps(t.TempDir(), nil)
	if _, err := a.setEnv(protocol.EnvSet{App: "flux", Set: map[string]string{"VERSION": "2.2.0", "ADMIN_PASSWORD": "kept"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.makeSecrets("flux", "miniflux"); err != nil {
		t.Fatal(err)
	}
	env, err := a.env("flux")
	if err != nil {
		t.Fatal(err)
	}
	compose, _ := catalog.Compose("miniflux")
	for _, name := range catalog.Secrets(compose) {
		if env[name] == "" {
			t.Errorf("%s was not made", name)
		}
	}
	if env["ADMIN_PASSWORD"] != "kept" || env["VERSION"] != "2.2.0" {
		t.Errorf("env = %v", env)
	}
	made := env["DB_PASSWORD"]
	if err := a.makeSecrets("flux", "miniflux"); err != nil {
		t.Fatal(err)
	}
	if again, _ := a.env("flux"); again["DB_PASSWORD"] != made {
		t.Error("a second deploy made the secret again")
	}
	if err := a.makeSecrets("flux", "no-such-app"); err == nil {
		t.Error("an app the catalog lacks made secrets")
	}
}

// TestMoveOldEnv: a server from beta.34 has the env only in the folder of
// the running release. The daemon copies that file to app.env when it
// starts and before a deploy, and a catalog app keeps its secrets.
func TestMoveOldEnv(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir, nil)
	compose, _ := catalog.Compose("miniflux")
	old := map[string]string{"VERSION": "2.2.0", "ODD": "a$b \"c\" d\\e\nf"}
	for _, name := range catalog.Secrets(compose) {
		old[name] = "old-" + name
	}
	// beta.34 wrote every value in double quotes, escaped like this.
	quote := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "\n", `\n`, "\r", `\r`).Replace
	beta34 := func(app string, number int, env map[string]string) {
		var lines strings.Builder
		for _, name := range slices.Sorted(maps.Keys(env)) {
			lines.WriteString(name + `="` + quote(env[name]) + "\"\n")
		}
		release := a.releaseDir(app, number)
		if err := os.MkdirAll(release, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(release, "app.env"), []byte(lines.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(a.appDir(app), "current"), []byte(strconv.Itoa(number)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	beta34("flux", 2, map[string]string{"VERSION": "2.1.0"})
	beta34("flux", 3, old)

	a.moveOldEnvs()
	info, err := os.Stat(a.envFile("flux"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
	if env, err := a.env("flux"); err != nil || !maps.Equal(env, old) {
		t.Errorf("env = %v, %v; want the one of the running release", env, err)
	}
	if keys := a.allEnvKeys()["flux"].Keys; !slices.Equal(keys, slices.Sorted(maps.Keys(old))) {
		t.Errorf("hello names %v", keys)
	}
	if err := a.makeSecrets("flux", "miniflux"); err != nil {
		t.Fatal(err)
	}
	if env, _ := a.env("flux"); !maps.Equal(env, old) {
		t.Errorf("the update made new secrets: %v", env)
	}

	// Before a deploy too, and only once: a value set since stays.
	beta34("blog", 1, map[string]string{"TOKEN": "old"})
	if err := a.ensureEnv("blog"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.setEnv(protocol.EnvSet{App: "blog", Set: map[string]string{"TOKEN": "new"}}); err != nil {
		t.Fatal(err)
	}
	a.moveOldEnvs()
	if err := a.ensureEnv("blog"); err != nil {
		t.Fatal(err)
	}
	if env, _ := a.env("blog"); env["TOKEN"] != "new" {
		t.Errorf("env = %v; the copy ran twice", env)
	}

	// An app without a release gets an empty file.
	if err := os.MkdirAll(a.appDir("fresh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := a.ensureEnv("fresh"); err != nil {
		t.Fatal(err)
	}
	if env, err := a.env("fresh"); err != nil || len(env) != 0 || !exists(a.envFile("fresh")) {
		t.Errorf("env = %v, %v", env, err)
	}
}
