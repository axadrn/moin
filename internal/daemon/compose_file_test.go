package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/axadrn/moin/protocol"
)

// composeRelease runs finish and pinNames on config, as a deploy of release
// 2 of my-app does after docker compose read the repo's file, and returns
// the compose file of that release.
func composeRelease(t *testing.T, a *apps, config map[string]any, detected protocol.Detected, domains *protocol.Domains) string {
	t.Helper()
	app := "my-app"
	start := protocol.DeployStart{App: app, Name: app, Number: 2, Domains: domains}
	release := &release{start: start, dir: a.releaseDir(app, 2), composeFile: filepath.Join(a.releaseDir(app, 2), "compose.json"),
		config: config, envFile: a.envFile(app), env: map[string]string{"DB_PASSWORD": "s3cret-value"}}
	if err := os.MkdirAll(release.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The project the app runs as, as switchProject wrote it.
	if err := os.WriteFile(filepath.Join(a.appDir(app), "project"), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}
	routed, err := route(app, detected, domains)
	if err != nil {
		t.Fatal(err)
	}
	release.detected = routed
	if err := finish(release); err != nil {
		t.Fatal(err)
	}
	pinned, err := readJSON(release.composeFile)
	if err != nil {
		t.Fatal(err)
	}
	pinNames(pinned, app, app, func(volume string) string { return "moin-0123abcd-" + volume })
	saveJSON(release.composeFile, pinned)
	saveJSON(filepath.Join(release.dir, "base.json"), release.config)
	saveJSON(filepath.Join(release.dir, "detected.json"), release.detected)
	text, err := a.portableCompose(app, 2)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// TestComposeFileRunsAnywhere: the compose file of an app with a build, a
// database, data, a config folder from the repo, domains and a password
// names no path of the server, keeps ${NAME} without its value, and brings
// the proxy, so its domains and HTTPS run elsewhere too.
func TestComposeFileRunsAnywhere(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir, nil)
	a.proxyMode = func() string { return "direct" }
	src := filepath.Join(a.releaseDir("my-app", 1), "src")
	config := map[string]any{
		"name": "moin-my-app",
		"services": map[string]any{
			"web": map[string]any{
				"build":       map[string]any{"context": src, "dockerfile": "Dockerfile"},
				"environment": map[string]any{"PORT": "8080", "DATABASE_URL": "postgres://app:${DB_PASSWORD}@db/app"},
				"env_file":    []any{map[string]any{"path": src + "/.env", "required": true}},
				"volumes": []any{
					map[string]any{"type": "bind", "source": filepath.Join(a.appDir("my-app"), "volumes", "data"), "target": "/data"},
					map[string]any{"type": "bind", "source": src + "/config", "target": "/config", "read_only": true},
				},
				"networks": map[string]any{"default": nil},
			},
			"db": map[string]any{
				"image":       "postgres:17",
				"environment": map[string]any{"POSTGRES_PASSWORD": "${DB_PASSWORD}"},
				"volumes":     []any{map[string]any{"type": "volume", "source": "pg", "target": "/var/lib/postgresql/data"}},
				"networks":    map[string]any{"default": nil},
			},
		},
		"volumes":  map[string]any{"pg": map[string]any{"name": "moin-my-app_pg"}},
		"networks": map[string]any{"default": map[string]any{"name": "moin-my-app_default"}},
	}
	hash := "ada:$2a$04$abcdefghijklmnopqrstuuJ0c7bQzZ1zCq3Yd0c6bVq1n8m9E2Qe"
	domains := &protocol.Domains{Domains: []string{"example.com", "www.example.com"}, Canonical: "example.com", Protect: hash}
	text := composeRelease(t, a, config, protocol.Detected{Kind: "compose", Service: "web", Port: 8080}, domains)

	for _, leak := range []string{dir, "s3cret-value", "abcdefghijklmnopqrstuuJ0c7bQzZ1zCq3Yd0c6bVq1n8m9E2Qe", "external"} {
		if strings.Contains(text, leak) {
			t.Errorf("the compose file holds %q:\n%s", leak, text)
		}
	}
	if !strings.Contains(text, "postgres://app:${DB_PASSWORD}@db/app") || !strings.HasPrefix(text, "name: my-app\nservices:\n") {
		t.Errorf("the compose file lost ${DB_PASSWORD} or its order:\n%s", text)
	}
	again, _ := a.portableCompose("my-app", 2)
	if again != text {
		t.Errorf("the same release gave another file:\n%s", again)
	}

	var file struct {
		Services map[string]struct {
			Image   string            `yaml:"image"`
			Build   map[string]any    `yaml:"build"`
			EnvFile []any             `yaml:"env_file"`
			Labels  map[string]string `yaml:"labels"`
			Volumes []any             `yaml:"volumes"`
			Ports   []any             `yaml:"ports"`
		} `yaml:"services"`
		Networks map[string]map[string]any `yaml:"networks"`
	}
	if err := yaml.Unmarshal([]byte(text), &file); err != nil {
		t.Fatal(err)
	}
	web := file.Services["web"]
	if web.Image != "" || web.Build["context"] != "." {
		t.Errorf("web does not build from the folder of the file: %+v", web)
	}
	if len(web.EnvFile) != 1 {
		t.Errorf("web reads %v, not one .env", web.EnvFile)
	}
	mounts, _ := json.Marshal(web.Volumes)
	for _, want := range []string{`"source":"./data"`, `"source":"./config"`} {
		if !strings.Contains(string(mounts), want) {
			t.Errorf("web lacks %s: %s", want, mounts)
		}
	}
	if got := web.Labels["traefik.http.middlewares.moin-my-app-auth.basicauth.usersfile"]; got != proxyUsers {
		t.Errorf("the password is read from %q", got)
	}
	if file.Services["db"].Image != "postgres:17" {
		t.Errorf("db runs %q", file.Services["db"].Image)
	}
	proxy := file.Services[proxyService]
	if proxy.Image != proxyImage || len(proxy.Ports) != 2 || !strings.Contains(text, "./htpasswd:/htpasswd:ro") || !strings.Contains(text, "certificatesresolvers.le.acme") {
		t.Errorf("the proxy is not as moin runs it: %+v", proxy)
	}
	if file.Networks[proxyNetwork]["name"] != proxyNetwork {
		t.Errorf("the proxy network is %v", file.Networks[proxyNetwork])
	}
}

// TestComposeFileWithoutDomains has no proxy, and the Dockerfile moin wrote
// for a Go repo comes inline.
func TestComposeFileWithoutDomains(t *testing.T) {
	a := newApps(t.TempDir(), nil)
	src := filepath.Join(a.releaseDir("my-app", 2), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Dockerfile"), []byte(goDockerfile(src, []byte("go 1.26\n"))), 0o644); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"services": map[string]any{"web": map[string]any{"build": map[string]any{"context": src, "dockerfile": "Dockerfile"}}}}
	text := composeRelease(t, a, config, protocol.Detected{Kind: "go", Service: "web", Port: 8080}, nil)
	if strings.Contains(text, "traefik") || !strings.Contains(text, "dockerfile_inline: |") || !strings.Contains(text, "FROM golang:1.26-bookworm") {
		t.Errorf("the compose file of a Go app without domains:\n%s", text)
	}
}

// TestComposeFileOfRailpack keeps the image moin built: the plan of a
// Railpack build is not in the repo.
func TestComposeFileOfRailpack(t *testing.T) {
	a := newApps(t.TempDir(), nil)
	src := filepath.Join(a.releaseDir("my-app", 2), "src")
	config := map[string]any{"services": map[string]any{"web": map[string]any{
		"build": map[string]any{"context": src, "dockerfile": "../" + railpackPlan, "args": map[string]any{"BUILDKIT_SYNTAX": railpackFrontend}},
	}}}
	text := composeRelease(t, a, config, protocol.Detected{Kind: "railpack", Service: "web", Port: 8080}, &protocol.Domains{Domains: []string{"example.com"}})
	if strings.Contains(text, "build:") || strings.Contains(text, "secrets") || !strings.Contains(text, "image: moin-my-app-web:2") {
		t.Errorf("the compose file of a Railpack app:\n%s", text)
	}
}
