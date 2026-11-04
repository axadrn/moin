package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/axadrn/ops/internal/client"
)

// TestCatalogCommands: :catalog install needs an app and a server, and
// :update refuses an app of your own.
func TestCatalogCommands(t *testing.T) {
	m := command(frames()["apps"], "catalog install umami")
	if m.err == nil || !strings.Contains(m.err.Error(), "<server>") {
		t.Errorf(":catalog install without a server: err %v", m.err)
	}
	m = command(frames()["apps"], "update")
	if m.err == nil || !strings.Contains(m.err.Error(), "not from the catalog") {
		t.Errorf(":update on an own app: err %v", m.err)
	}
}

func TestCatalogOutput(t *testing.T) {
	now := time.Now()
	released := now.Add(-2 * time.Hour)
	out := catalogOutput(1, []client.CatalogEntry{
		{Slug: "umami", Category: "Analytics", Database: "postgres", Pitch: "Analytics", Latest: &client.CatalogVersion{Tag: "3.4.1", Date: &released}},
		{Slug: "memos", Category: "Productivity", Pitch: "Notes"},
	}, now)
	text := strings.Join(out.lines, "\n")
	if !strings.Contains(text, "3.4.1, 2h 0m ago") || !strings.Contains(text, "memos") || out.title != "catalog" {
		t.Errorf("output:\n%s", text)
	}
}
