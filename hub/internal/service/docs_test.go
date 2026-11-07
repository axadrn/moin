package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsShowTheRealHub(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	install := "---\ntitle: Start\n---\n\n```bash\ncurl -fsSL https://moin.example.com/cli.sh | sh\n```\n\nInstead of `moin.example.com`.\n"
	for _, name := range []string{"getting-started.md", "self-hosting.md"} {
		if err := os.WriteFile(filepath.Join(docs, name), []byte(install), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	service := NewDocsService(root, "https://moin.run/")
	if err := service.BuildDocsTree(); err != nil {
		t.Fatal(err)
	}
	page, err := service.DocPage("getting-started")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page.Content, "example.com") || !strings.Contains(page.Content, "https://moin.run/cli.sh") || !strings.Contains(page.HTMLContent, "moin.run") {
		t.Fatalf("getting started still shows the placeholder:\n%s", page.Content)
	}
	selfHosting, err := service.DocPage("self-hosting")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(selfHosting.Content, "https://moin.example.com") {
		t.Fatalf("self-hosting lost its placeholder:\n%s", selfHosting.Content)
	}
}
