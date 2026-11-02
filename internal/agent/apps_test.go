package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/axadrn/ops/protocol"
)

func tarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	compressed := gzip.NewWriter(&out)
	archive := tar.NewWriter(compressed)
	for name, body := range files {
		err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = archive.Write([]byte(body))
	}
	archive.Close()
	compressed.Close()
	return out.Bytes()
}

func TestUnpackRefusesPathsOutsideTheSource(t *testing.T) {
	for _, name := range []string{"../evil", "a/../../evil", "/etc/evil"} {
		dir := t.TempDir()
		err := unpack(bytes.NewReader(tarball(t, map[string]string{name: "x"})), filepath.Join(dir, "src"), false)
		if name == "/etc/evil" {
			// An absolute name lands inside the source directory.
			if _, statErr := os.Stat(filepath.Join(dir, "src", "etc", "evil")); err != nil || statErr != nil {
				t.Fatalf("%s: err = %v, stat = %v", name, err, statErr)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "leaves the source directory") {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestGenerateFindsTheMainPackage(t *testing.T) {
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "go.mod"), []byte("module x\n\ngo 1.26.3\n"), 0o644)
	os.MkdirAll(filepath.Join(src, "cmd", "web"), 0o755)
	os.WriteFile(filepath.Join(src, "cmd", "web", "main.go"), []byte("package main\n"), 0o644)
	kind, err := generate(src)
	if err != nil || kind != "go" {
		t.Fatalf("generate = %q, %v", kind, err)
	}
	dockerfile, _ := os.ReadFile(filepath.Join(src, "Dockerfile"))
	if !strings.Contains(string(dockerfile), "golang:1.26-bookworm") || !strings.Contains(string(dockerfile), "go build -o /app ./cmd/web") {
		t.Fatalf("Dockerfile:\n%s", dockerfile)
	}
	if composePath(src) == "" {
		t.Fatal("generate wrote no compose.yml")
	}
	if _, err := generate(t.TempDir()); err == nil {
		t.Fatal("an empty repo must fail")
	}
}

func TestMainServiceAndPort(t *testing.T) {
	services := map[string]any{
		"web":   map[string]any{"build": map[string]any{}, "ports": []any{map[string]any{"target": float64(3000)}}},
		"redis": map[string]any{"image": "redis"},
	}
	if got := mainService(services, nil); got != "web" {
		t.Fatalf("main service = %q", got)
	}
	if got := mainService(services, &protocol.Config{Service: "redis"}); got != "redis" {
		t.Fatalf("x-ops service = %q", got)
	}
	if got := mainService(services, &protocol.Config{Service: "nope"}); got != "" {
		t.Fatalf("unknown x-ops service = %q", got)
	}
	if got := mainPort(t.TempDir(), services["web"], nil); got != 3000 {
		t.Fatalf("port = %d", got)
	}
	if got := mainPort(t.TempDir(), services["web"], &protocol.Config{Port: 9000}); got != 9000 {
		t.Fatalf("x-ops port = %d", got)
	}
}

func TestSQLiteFiles(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755)
	header := make([]byte, 100)
	copy(header, "SQLite format 3\x00\x10\x00")
	os.WriteFile(filepath.Join(dir, "a", "b", "app.db"), header, 0o644)
	os.WriteFile(filepath.Join(dir, "notes.db"), []byte("not a database"), 0o644)
	os.WriteFile(filepath.Join(dir, "stub.db"), []byte("SQLite format 3\x00"), 0o644)
	files := sqliteFiles(dir)
	if len(files) != 1 || !strings.HasSuffix(files[0], "app.db") {
		t.Fatalf("files = %v", files)
	}
}

func TestFinishImageDeployKeepsTheOtherImages(t *testing.T) {
	base := map[string]any{"services": map[string]any{
		"web":    map[string]any{"build": map[string]any{"context": "."}},
		"worker": map[string]any{"build": map[string]any{"context": "."}},
		"redis":  map[string]any{"image": "redis:7"},
	}}
	file := filepath.Join(t.TempDir(), "compose.json")
	start := protocol.DeployStart{App: "blog", Number: 4, Image: "ghcr.io/me/blog:abc", Env: map[string]string{"KEY": "a$b"}}
	running := map[string]string{"web": "ghcr.io/me/blog:old", "worker": "ops-blog-worker:3", "redis": "redis:7"}
	err := finish(base, start, protocol.Detected{Service: "web"}, running, file)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(file)
	var config struct {
		Services map[string]struct {
			Build       any               `json:"build"`
			Image       string            `json:"image"`
			Environment map[string]string `json:"environment"`
			Restart     string            `json:"restart"`
		} `json:"services"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	web, worker, redis := config.Services["web"], config.Services["worker"], config.Services["redis"]
	if web.Image != "ghcr.io/me/blog:abc" || web.Build != nil || web.Environment["KEY"] != "a$$b" {
		t.Fatalf("web = %+v", web)
	}
	if worker.Image != "ops-blog-worker:3" || worker.Build != nil || worker.Environment["KEY"] == "" {
		t.Fatalf("worker = %+v", worker)
	}
	if redis.Image != "redis:7" || redis.Environment != nil || redis.Restart != "unless-stopped" {
		t.Fatalf("redis = %+v", redis)
	}
	if builds(base) != true {
		t.Fatal("finish changed the base")
	}
}

// A restart after docker image prune builds the gone image again from the
// kept source instead of pulling it; with the source gone too it says what
// to do.
func TestRestartRebuildsMissingImages(t *testing.T) {
	bin := t.TempDir()
	// A docker that has every image but ops-blog-web:3.
	script := "#!/bin/sh\ncase \"$*\" in\n*ops-blog-web:3*) exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	source := t.TempDir()
	base := map[string]any{"services": map[string]any{
		"web":    map[string]any{"build": map[string]any{"context": source}},
		"worker": map[string]any{"build": map[string]any{"context": source}},
	}}
	running := map[string]string{"web": "ops-blog-web:3", "worker": "ops-blog-worker:3"}
	var lines []string
	logf := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	a := &apps{}
	if err := a.rebuildMissing(context.Background(), base, running, "web", false, logf); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "compose.json")
	if err := finish(base, protocol.DeployStart{App: "blog", Number: 5}, protocol.Detected{Service: "web"}, running, file); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(file)
	var config struct {
		Services map[string]struct {
			Build any    `json:"build"`
			Image string `json:"image"`
		} `json:"services"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatal(err)
	}
	web, worker := config.Services["web"], config.Services["worker"]
	if web.Image != "ops-blog-web:5" || web.Build == nil || worker.Image != "ops-blog-worker:3" || worker.Build != nil {
		t.Fatalf("web = %+v, worker = %+v", web, worker)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "building it again") {
		t.Fatalf("log = %q", lines)
	}

	gone := map[string]any{"services": map[string]any{"web": map[string]any{"build": map[string]any{"context": filepath.Join(source, "gone")}}}}
	err := a.rebuildMissing(context.Background(), gone, map[string]string{"web": "ops-blog-web:3"}, "web", false, logf)
	if err == nil || !strings.Contains(err.Error(), "deploy again with ops deploy") {
		t.Fatalf("err = %v", err)
	}
}

func TestRoutingLabels(t *testing.T) {
	detected, err := route(protocol.Detected{Service: "web", Port: 8080, Config: &protocol.Config{Canonical: "blog.de"}}, &protocol.Routing{Domains: []string{"Blog.de", "www.blog.de"}, Protect: "me:$2y$05$abc"})
	if err != nil {
		t.Fatal(err)
	}
	if detected.Domains[0] != "blog.de" || detected.Canonical != "blog.de" || !detected.Protected {
		t.Fatalf("detected = %+v", detected)
	}
	config := map[string]any{"services": map[string]any{"web": map[string]any{"image": "x"}}}
	service := config["services"].(map[string]any)["web"].(map[string]any)
	addRouting(config, service, "blog", detected, "me:$2y$05$abc")
	labels := service["labels"].(map[string]any)
	if labels["traefik.http.routers.ops-blog.rule"] != "Host(`blog.de`) || Host(`www.blog.de`)" {
		t.Fatalf("rule = %v", labels["traefik.http.routers.ops-blog.rule"])
	}
	if labels["traefik.http.routers.ops-blog.middlewares"] != "ops-blog-canonical,ops-blog-auth" {
		t.Fatalf("middlewares = %v", labels["traefik.http.routers.ops-blog.middlewares"])
	}
	if labels["traefik.http.middlewares.ops-blog-auth.basicauth.users"] != "me:$$2y$$05$$abc" {
		t.Fatalf("auth = %v", labels["traefik.http.middlewares.ops-blog-auth.basicauth.users"])
	}
	if labels["traefik.http.middlewares.ops-blog-canonical.redirectregex.regex"] != `^https?://(www\.blog\.de)(:[0-9]+)?(.*)` {
		t.Fatalf("regex = %v", labels["traefik.http.middlewares.ops-blog-canonical.redirectregex.regex"])
	}
	if _, ok := service["networks"].(map[string]any)[proxyNetwork]; !ok {
		t.Fatal("the main service is not on the proxy network")
	}
	if _, err := route(protocol.Detected{Config: &protocol.Config{Protect: true}}, nil); err == nil {
		t.Fatal("protect without password must fail")
	}
	if _, err := route(protocol.Detected{}, &protocol.Routing{Domains: []string{"not a domain"}}); err == nil {
		t.Fatal("a bad domain must fail")
	}
}

func TestStartKeepsTheCrashInTheDeployLog(t *testing.T) {
	// A docker that starts the app, finds it not running and has its last
	// log line.
	bin := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in\n*\" logs \"*) echo \"web-1  | panic: boom\" ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var lines []string
	logf := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	a := &apps{}
	err := a.start(context.Background(), "blog", "compose.yaml", protocol.Detected{Service: "web"}, false, logf)
	if err == nil || !strings.Contains(err.Error(), "see the deploy log") {
		t.Fatalf("err = %v", err)
	}
	if !slices.Contains(lines, "web-1  | panic: boom") {
		t.Fatalf("deploy log = %q, want the crash", lines)
	}
}

func TestPruneKeepsBuildsAndTheRestartsAfterThem(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no docker: prune leaves images alone
	a := &apps{dir: t.TempDir()}
	// v1 to v6 are builds, v7 to v13 restarts, v14 a failed deploy, and
	// v2 a build from before the kind file.
	for number := 1; number <= 14; number++ {
		release := a.releaseDir("blog", number)
		if err := os.MkdirAll(release, 0o700); err != nil {
			t.Fatal(err)
		}
		kind := releaseBuild
		switch {
		case number == 2:
			// From before the kind file, with a source: a build.
			if err := os.MkdirAll(filepath.Join(release, "src"), 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		case number == 14:
			kind = releaseFailed
		case number > 6:
			kind = releaseApply
		}
		if err := os.WriteFile(filepath.Join(release, "kind"), []byte(kind), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a.appDir("blog"), "current"), []byte("13"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.prune(context.Background(), "blog")
	var kept []int
	for number := 1; number <= 14; number++ {
		if _, err := os.Stat(a.releaseDir("blog", number)); err == nil {
			kept = append(kept, number)
		}
	}
	if want := []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}; !slices.Equal(kept, want) {
		t.Fatalf("kept %v, want %v", kept, want)
	}
}
