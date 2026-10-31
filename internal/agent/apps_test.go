package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
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
		err := unpack(bytes.NewReader(tarball(t, map[string]string{name: "x"})), filepath.Join(dir, "src"))
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
	os.WriteFile(filepath.Join(dir, "a", "b", "app.db"), []byte("SQLite format 3\x00rest"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.db"), []byte("not a database"), 0o644)
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
	err := finish(base, start, protocol.Detected{Service: "web"}, 3, file)
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
