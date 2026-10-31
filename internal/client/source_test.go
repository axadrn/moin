package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadLocalConfig(t *testing.T) {
	dir := t.TempDir()
	compose := "services:\n  web:\n    build: .\n\nx-ops:\n  app: blog # the name\n  server: \"web-1\"\n  domains: [blog.de]\n\nvolumes:\n  app: {}\n"
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte(compose), 0o644)
	config := ReadLocalConfig(dir)
	if config.App != "blog" || config.Server != "web-1" {
		t.Fatalf("config = %+v", config)
	}
	if got := ReadLocalConfig(t.TempDir()); got != (LocalConfig{}) {
		t.Fatalf("no compose.yml: %+v", got)
	}
}
