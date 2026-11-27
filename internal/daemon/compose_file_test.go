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
// the compose file of that release. start brings the domains and an image;
// running names the images of the running release, for a deploy without
// source, such as a restart or a rollback.
func composeRelease(t *testing.T, a *apps, config map[string]any, detected protocol.Detected, start protocol.DeployStart, running map[string]string) (string, error) {
	t.Helper()
	app := "my-app"
	start.App, start.Name, start.Number = app, app, 2
	release := &release{start: start, dir: a.releaseDir(app, 2), composeJSON: filepath.Join(a.releaseDir(app, 2), "compose.json"),
		config: config, running: running, envFile: a.envFile(app), env: map[string]string{"DB_PASSWORD": "s3cret-value"}}
	if err := os.MkdirAll(release.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The project the app runs as, as switchProject wrote it.
	if err := os.WriteFile(filepath.Join(a.appDir(app), "project"), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}
	routed, err := route(app, detected, start.Domains)
	if err != nil {
		t.Fatal(err)
	}
	release.detected = routed
	if err := finish(release); err != nil {
		t.Fatal(err)
	}
	pinned, err := readJSON(release.composeJSON)
	if err != nil {
		t.Fatal(err)
	}
	pinNames(pinned, app, app, func(volume string) string { return "moin-0123abcd-" + volume })
	saveJSON(release.composeJSON, pinned)
	saveJSON(filepath.Join(release.dir, "base.json"), release.config)
	saveJSON(filepath.Join(release.dir, "detected.json"), release.detected)
	return a.composeFile(app, 2)
}

// mustCompose is composeRelease for a file that must be made.
func mustCompose(t *testing.T, a *apps, config map[string]any, detected protocol.Detected, start protocol.DeployStart, running map[string]string) string {
	t.Helper()
	text, err := composeRelease(t, a, config, detected, start, running)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// webBuild is a compose file with one service web, built from src.
func webBuild(src string) map[string]any {
	return map[string]any{"services": map[string]any{"web": map[string]any{"build": map[string]any{"context": src, "dockerfile": "Dockerfile"}}}}
}

// TestComposeFileRunsAnywhere: the compose file of an app with a build, a
// database, data, a config folder from the repo and domains names no path of the server, keeps ${NAME} without its value, and brings
// the proxy, so its domains and HTTPS run elsewhere too.
func TestComposeFileRunsAnywhere(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir)
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
	domains := &protocol.Domains{Domains: []string{"example.com", "www.example.com"}}
	text := mustCompose(t, a, config, protocol.Detected{Kind: "compose", Service: "web", Port: 8080}, protocol.DeployStart{Domains: domains}, nil)

	for _, leak := range []string{dir, "s3cret-value", "external"} {
		if strings.Contains(text, leak) {
			t.Errorf("the compose file holds %q:\n%s", leak, text)
		}
	}
	if !strings.Contains(text, "postgres://app:${DB_PASSWORD}@db/app") || !strings.HasPrefix(text, "name: my-app\nservices:\n") {
		t.Errorf("the compose file lost ${DB_PASSWORD} or its order:\n%s", text)
	}
	again, _ := a.composeFile("my-app", 2)
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
	if file.Services["db"].Image != "postgres:17" {
		t.Errorf("db runs %q", file.Services["db"].Image)
	}
	proxy := file.Services[proxyService]
	if proxy.Image != proxyImage || len(proxy.Ports) != 2 || !strings.Contains(text, "certificatesresolvers.le.acme") {
		t.Errorf("the proxy is not as moin runs it: %+v", proxy)
	}
	if file.Networks[proxyNetwork]["name"] != proxyNetwork {
		t.Errorf("the proxy network is %v", file.Networks[proxyNetwork])
	}
}

// TestComposeFileWithoutDomains has no proxy, and the Dockerfile moin
// wrote into the compose file of a Go repo stays in it.
func TestComposeFileWithoutDomains(t *testing.T) {
	a := newApps(t.TempDir())
	src := filepath.Join(a.releaseDir("my-app", 2), "src")
	config := map[string]any{"services": map[string]any{"web": map[string]any{"build": map[string]any{"context": src, "dockerfile_inline": goDockerfile(src, []byte("go 1.26\n"))}}}}
	text := mustCompose(t, a, config, protocol.Detected{Kind: "go", Service: "web", Port: 8080}, protocol.DeployStart{}, nil)
	if strings.Contains(text, "traefik") || strings.Contains(text, proxyNetwork) || !strings.Contains(text, "dockerfile_inline: |") || !strings.Contains(text, "FROM golang:1.26-bookworm") {
		t.Errorf("the compose file of a Go app without domains:\n%s", text)
	}
}

// TestComposeFileOfRailpack keeps the image moin built: the plan of a
// Railpack build is not in the repo. Its secrets go with the build.
func TestComposeFileOfRailpack(t *testing.T) {
	a := newApps(t.TempDir())
	src := filepath.Join(a.releaseDir("my-app", 2), "src")
	config := map[string]any{"services": map[string]any{"web": map[string]any{
		"build": map[string]any{"context": src, "dockerfile": "../" + railpackPlan, "args": map[string]any{"BUILDKIT_SYNTAX": railpackFrontend}},
	}}}
	text := mustCompose(t, a, config, protocol.Detected{Kind: "railpack", Service: "web", Port: 8080}, protocol.DeployStart{Domains: &protocol.Domains{Domains: []string{"example.com"}}}, nil)
	if strings.Contains(text, "build:") || strings.Contains(text, "secrets") || !strings.Contains(text, "image: moin-my-app-web:2") {
		t.Errorf("the compose file of a Railpack app:\n%s", text)
	}
}

// TestComposeFileAfterARestart: a restart or rollback runs the images of
// the running release, so compose.json names moin's image, not the build.
// The compose file builds from "." again, from base.json.
func TestComposeFileAfterARestart(t *testing.T) {
	a := newApps(t.TempDir())
	src := filepath.Join(a.releaseDir("my-app", 1), "src")
	text := mustCompose(t, a, webBuild(src), protocol.Detected{Kind: "dockerfile", Service: "web", Port: 8080}, protocol.DeployStart{}, map[string]string{"web": "moin-my-app-web:1"})
	compose, _ := os.ReadFile(filepath.Join(a.releaseDir("my-app", 2), "compose.json"))
	if !strings.Contains(string(compose), `"image":"moin-my-app-web:1"`) || strings.Contains(string(compose), `"build"`) {
		t.Fatalf("compose.json of the restart builds:\n%s", compose)
	}
	if strings.Contains(text, "image:") || !strings.Contains(text, "context: .") {
		t.Errorf("the compose file after a restart:\n%s", text)
	}
}

// TestComposeFileOfAnImageDeploy keeps the image CI built for a service the
// repo builds, pinned as it was deployed.
func TestComposeFileOfAnImageDeploy(t *testing.T) {
	a := newApps(t.TempDir())
	src := filepath.Join(a.releaseDir("my-app", 2), "src")
	image := "ghcr.io/me/my-app@sha256:" + strings.Repeat("a", 64)
	text := mustCompose(t, a, webBuild(src), protocol.Detected{Kind: "dockerfile", Service: "web", Port: 8080}, protocol.DeployStart{Image: image}, nil)
	if strings.Contains(text, "build:") || !strings.Contains(text, "image: "+image) {
		t.Errorf("the compose file of an image deploy:\n%s", text)
	}
}

// TestComposeFileProxyName: an app with a service named proxy gets the
// proxy as moin-traefik; with both names taken, the file is not made, and
// no service is replaced.
func TestComposeFileProxyName(t *testing.T) {
	a := newApps(t.TempDir())
	src := filepath.Join(a.releaseDir("my-app", 2), "src")
	domains := protocol.DeployStart{Domains: &protocol.Domains{Domains: []string{"example.com"}}}
	config := webBuild(src)
	config["services"].(map[string]any)[proxyService] = map[string]any{"image": "nginx:1.27"}
	text := mustCompose(t, a, config, protocol.Detected{Kind: "compose", Service: "web", Port: 8080}, domains, nil)
	var file struct {
		Services map[string]struct {
			Image string `yaml:"image"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(text), &file); err != nil {
		t.Fatal(err)
	}
	if file.Services[proxyService].Image != "nginx:1.27" || file.Services[proxyName].Image != proxyImage {
		t.Errorf("the proxy next to a service named proxy:\n%s", text)
	}
	config = webBuild(src)
	config["services"].(map[string]any)[proxyService] = map[string]any{"image": "nginx:1.27"}
	config["services"].(map[string]any)[proxyName] = map[string]any{"image": "caddy:2"}
	if text, err := composeRelease(t, a, config, protocol.Detected{Kind: "compose", Service: "web", Port: 8080}, domains, nil); err == nil {
		t.Errorf("both names taken, yet a file:\n%s", text)
	}
}

// TestComposeFileLeavesTheInstantAddress: the instant address names this
// server, so its routers stay with moin. The domains keep theirs, and an
// app reached only there gets no proxy.
func TestComposeFileLeavesTheInstantAddress(t *testing.T) {
	a := newApps(t.TempDir())
	src := filepath.Join(a.releaseDir("my-app", 2), "src")
	detected := protocol.Detected{Kind: "compose", Service: "web", Port: 8080}
	instant := protocol.Domains{Instant: "sslip.io", InstantIP: "203.0.113.7"}
	text := mustCompose(t, a, webBuild(src), detected, protocol.DeployStart{Domains: &instant}, nil)
	compose, _ := os.ReadFile(filepath.Join(a.releaseDir("my-app", 2), "compose.json"))
	if !strings.Contains(string(compose), "203-0-113-7.sslip.io") {
		t.Fatalf("compose.json has no instant address:\n%s", compose)
	}
	if strings.Contains(text, "sslip.io") || strings.Contains(text, "traefik") || strings.Contains(text, proxyNetwork) {
		t.Errorf("an app reached only at its instant address:\n%s", text)
	}
	instant.Domains = []string{"example.com"}
	text = mustCompose(t, a, webBuild(src), detected, protocol.DeployStart{Domains: &instant}, nil)
	if strings.Contains(text, "sslip.io") || strings.Contains(text, "-instant") || !strings.Contains(text, "Host(`example.com`)") || !strings.Contains(text, "image: "+proxyImage) {
		t.Errorf("an app with a domain and its instant address:\n%s", text)
	}
}

// TestComposeFileOnATunnelServer says in its first line that the proxy
// takes ports 80 and 443 here, which a tunnel server opens none of.
func TestComposeFileOnATunnelServer(t *testing.T) {
	a := newApps(t.TempDir())
	a.proxyMode = func() string { return "tunnel" }
	src := filepath.Join(a.releaseDir("my-app", 2), "src")
	text := mustCompose(t, a, webBuild(src), protocol.Detected{Kind: "compose", Service: "web", Port: 8080}, protocol.DeployStart{Domains: &protocol.Domains{Domains: []string{"example.com"}}}, nil)
	first, _, _ := strings.Cut(text, "\n")
	if !strings.HasPrefix(first, "# ") || !strings.Contains(first, "Cloudflare Tunnel") || !strings.Contains(text, "--certificatesresolvers.le.acme.httpchallenge=true") {
		t.Errorf("the compose file of a tunnel server:\n%s", text)
	}
}

// TestBuildAgainDropsTheSecretsHash: the hash of the env values moin hands
// a build stays on the server.
func TestBuildAgainDropsTheSecretsHash(t *testing.T) {
	service := map[string]any{"image": "moin-my-app-web:2", "build": map[string]any{"context": "/x", "args": map[string]any{"secrets-hash": "abc", "VERSION": "1"}}}
	buildAgain("my-app", "web", service, nil, false)
	args := service["build"].(map[string]any)["args"].(map[string]any)
	if _, ok := args["secrets-hash"]; ok || args["VERSION"] != "1" {
		t.Errorf("args = %v", args)
	}
	service = map[string]any{"image": "moin-my-app-web:2", "build": map[string]any{"context": "/x", "args": map[string]any{"secrets-hash": "abc"}}}
	buildAgain("my-app", "web", service, nil, false)
	if _, ok := service["build"].(map[string]any)["args"]; ok {
		t.Errorf("build = %v", service["build"])
	}
}

// TestComposeFileHoldsNoSecret: a compose file that holds a value of the
// app's env is not made. A value where the deploy
// fills it in on purpose, such as the version in the image or a domain in
// the proxy's labels, is fine, and so is a short one.
func TestComposeFileHoldsNoSecret(t *testing.T) {
	for _, test := range []struct {
		name, url string
		held      string
	}{
		{"names", "postgres://u:${DB_PASSWORD}@db/x", ""},
		{"value", "postgres://u:s3cret$value@db/x", "the value of DB_PASSWORD"},
		{"value as compose writes it", "postgres://u:s3cret$$value@db/x", "the value of DB_PASSWORD"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newApps(t.TempDir())
			env := map[string]string{"DB_PASSWORD": "s3cret$value", "VERSION": "2026.10.3", "DOMAIN": "my-app.example.com", "DEBUG": "true"}
			if _, err := a.setEnv(protocol.EnvSet{App: "my-app", Set: env}); err != nil {
				t.Fatal(err)
			}
			config := map[string]any{"services": map[string]any{"web": map[string]any{
				"image":       "ghcr.io/me/my-app:2026.10.3",
				"environment": map[string]any{"DATABASE_URL": test.url, "LOG": "true"},
			}}}
			start := protocol.DeployStart{Domains: &protocol.Domains{Domains: []string{"my-app.example.com"}}}
			text, err := composeRelease(t, a, config, protocol.Detected{Kind: "compose", Service: "web", Port: 8080}, start, nil)
			if test.held == "" {
				if err != nil || !strings.Contains(text, "${DB_PASSWORD}") {
					t.Fatalf("not made: %v\n%s", err, text)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "it holds "+test.held+", so it stays on the server") || text != "" {
				t.Fatalf("made: %v\n%s", err, text)
			}
		})
	}
}
