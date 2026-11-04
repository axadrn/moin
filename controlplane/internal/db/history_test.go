package db

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestHistoryConnection: history.db sits next to the database; one in memory
// keeps its history in memory.
func TestHistoryConnection(t *testing.T) {
	got := HistoryConnection("./data/ops.db?_pragma=foreign_keys(1)")
	if !strings.HasPrefix(got, filepath.Join("data", "history.db")+"?") {
		t.Errorf("file database: %q", got)
	}
	if got := HistoryConnection("file:/var/lib/ops/ops.db"); !strings.HasPrefix(got, "/var/lib/ops/history.db?") {
		t.Errorf("file: prefix: %q", got)
	}
	for _, memory := range []string{":memory:", "file::memory:?cache=shared", ""} {
		if got := HistoryConnection(memory); got != ":memory:" {
			t.Errorf("%q: %q", memory, got)
		}
	}
	history, err := OpenHistory(HistoryConnection(filepath.Join(t.TempDir(), "ops.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	for _, insert := range []string{
		`INSERT INTO metrics_minutes (target, minute) VALUES ('a', 60)`,
		`INSERT INTO uptime_hours (app_id, domain, hour) VALUES ('a', 'a.example', '2026-11-04 10:00:00')`,
	} {
		if _, err := history.Exec(insert); err != nil {
			t.Errorf("%s: %v", insert, err)
		}
	}
}
