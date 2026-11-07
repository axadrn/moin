package db

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestHistoryConnection: history.db sits next to the database; one in memory
// keeps its history in memory.
func TestHistoryConnection(t *testing.T) {
	got := HistoryConnection("./data/moin.db?_pragma=foreign_keys(1)")
	if !strings.HasPrefix(got, filepath.Join("data", "history.db")+"?") {
		t.Errorf("file database: %q", got)
	}
	if got := HistoryConnection("file:/var/lib/moin/moin.db"); !strings.HasPrefix(got, "/var/lib/moin/history.db?") {
		t.Errorf("file: prefix: %q", got)
	}
	for _, memory := range []string{":memory:", "file::memory:?cache=shared", ""} {
		if got := HistoryConnection(memory); got != ":memory:" {
			t.Errorf("%q: %q", memory, got)
		}
	}
	history, err := OpenHistory(HistoryConnection(filepath.Join(t.TempDir(), "moin.db")))
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

// TestHistoryCountsVisits: a history.db from before keeps its uptime
// counts, as visits.
func TestHistoryCountsVisits(t *testing.T) {
	connection := HistoryConnection(filepath.Join(t.TempDir(), "moin.db"))
	old, err := Init(connection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE uptime_hours (app_id TEXT NOT NULL, domain TEXT NOT NULL, hour TIMESTAMP NOT NULL, checks INTEGER NOT NULL DEFAULT 0, up INTEGER NOT NULL DEFAULT 0, response_ms INTEGER NOT NULL DEFAULT 0, max_ms INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (app_id, domain, hour));
		INSERT INTO uptime_hours (app_id, domain, hour, checks, up) VALUES ('a', 'a.example', '2026-11-04 10:00:00', 60, 59)`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	history, err := OpenHistory(connection)
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	var visits int
	if err := history.Get(&visits, `SELECT visits FROM uptime_hours`); err != nil || visits != 60 {
		t.Fatalf("visits = %d, %v", visits, err)
	}
}
