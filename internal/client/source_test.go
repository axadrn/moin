package client

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestReadLocalConfig(t *testing.T) {
	dir := t.TempDir()
	compose := "services:\n  web:\n    build: .\n\nx-moin:\n  app: blog # the name\n  server: \"web-1\"\n  domains: [blog.de]\n\nvolumes:\n  app: {}\n"
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte(compose), 0o644)
	config := ReadLocalConfig(dir)
	if config.App != "blog" || config.Server != "web-1" {
		t.Fatalf("config = %+v", config)
	}
	// The block of the old name counts for nothing: the agent refuses it.
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("x-ops:\n  app: old\n"), 0o644)
	if got := ReadLocalConfig(dir); got.App != "" {
		t.Fatalf("x-ops read: %+v", got)
	}
	if got := ReadLocalConfig(t.TempDir()); got != (LocalConfig{}) {
		t.Fatalf("no compose.yml: %+v", got)
	}
}

func TestPackFollowsALinkedDirectory(t *testing.T) {
	project := t.TempDir()
	os.WriteFile(filepath.Join(project, "Dockerfile"), []byte("FROM scratch\n"), 0o644)
	link := filepath.Join(t.TempDir(), "current")
	if err := os.Symlink(project, link); err != nil {
		t.Skip(err)
	}
	var out bytes.Buffer
	files, err := Pack(link, &out)
	if err != nil || files != 1 {
		t.Fatalf("Pack = %d, %v", files, err)
	}
	compressed, _ := gzip.NewReader(&out)
	header, err := tar.NewReader(compressed).Next()
	if err != nil || header.Name != "Dockerfile" {
		t.Fatalf("first file = %v, %v", header, err)
	}
}
