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

// TestMoveOldEnvFromBeta34: testdata/beta34 holds release folders beta.34
// wrote with its own deploy code, up to the start of the containers, for a
// server whose state folder is /var/lib/moin. A deploy without
// source, such as a restart or an env change, and a rollback wrote no
// app.env: the env is in compose.json only. The daemon takes it from there,
// with no key of the compose file's own, and a catalog app keeps its
// passwords through an update.
func TestMoveOldEnvFromBeta34(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata/beta34")); err != nil {
		t.Fatal(err)
	}
	a := newApps(dir, nil)
	a.moveOldEnvs()
	want := map[string]map[string]string{
		// Deployed without env, then env set and restarted.
		"echo": {"GREETING": "hello-greeting", "API_TOKEN": "token-value"},
		// Restarted with the env of the deploy: SECRET_KEY: ${SECRET_KEY}
		// has the value base.json has, and v1's app.env names it. TZ: UTC
		// is the compose file's.
		"blog": {"SECRET_KEY": "secret-key-value", "TOKEN": "token-value", "ODD": "a$b \"c\" d\\e\nf"},
		// miniflux from the catalog, restarted: DATABASE_URL and the
		// others are the compose file's.
		"feeds": {"VERSION": "2.3.4", "DB_PASSWORD": "db-password-value", "ADMIN_PASSWORD": "admin-password-value"},
		// v2 changed the env, v3 deployed from source, v4 rolled back to
		// v2: the env v2 ran with.
		"roll": {"A": "two", "B": "bee"},
	}
	for app, env := range want {
		got, err := a.env(app)
		if err != nil || !maps.Equal(got, env) {
			t.Errorf("%s: env = %v, %v; want %v", app, got, err, env)
		}
		if info, err := os.Stat(a.envFile(app)); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: app.env %v, %v", app, info, err)
		}
	}
	// Postgres runs with the password the env has now, and a catalog
	// update makes no new one.
	compose, err := readJSON(filepath.Join(a.releaseDir("feeds", 2), "compose.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := serviceEnvironment(compose, "db")["POSTGRES_PASSWORD"]; got != want["feeds"]["DB_PASSWORD"] {
		t.Errorf("Postgres runs with %v", got)
	}
	if err := a.makeSecrets("feeds", "miniflux"); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.env("feeds"); !maps.Equal(got, want["feeds"]) {
		t.Errorf("the update made new secrets: %v", got)
	}
}

// TestMoveOldEnvSaysWhy: without compose.json the daemon has no env to
// take; it says so, and the deploy starts with an empty one. Without
// base.json it takes only the keys an older app.env names.
func TestMoveOldEnvSaysWhy(t *testing.T) {
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata/beta34")); err != nil {
		t.Fatal(err)
	}
	a := newApps(dir, nil)
	if err := os.Remove(filepath.Join(a.releaseDir("echo", 2), "compose.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(a.releaseDir("feeds", 2), "base.json")); err != nil {
		t.Fatal(err)
	}
	out := journal(t)
	a.moveOldEnvs()
	log := out.String()
	for _, want := range []string{
		`level=WARN msg="no env found in the running release`,
		`app=echo release=2`,
		`level=WARN msg="env taken from the running release without its base.json`,
		`app=feeds release=2 from=releases/2/compose.json keys="[ADMIN_PASSWORD DB_PASSWORD VERSION]"`,
		`level=INFO msg="env taken from the running release" app=blog release=2 from=releases/2/compose.json keys="[ODD SECRET_KEY TOKEN]"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log has no %q:\n%s", want, log)
		}
	}
	if exists(a.envFile("echo")) {
		t.Error("echo got an env file")
	}
	if err := a.ensureEnv("echo"); err != nil {
		t.Fatal(err)
	}
	if env, err := a.env("echo"); err != nil || len(env) != 0 || !exists(a.envFile("echo")) {
		t.Errorf("env = %v, %v", env, err)
	}
}
