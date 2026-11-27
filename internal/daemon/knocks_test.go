package daemon

import (
	"testing"
	"time"
)

// Each window counts what it should, from reads across a year. Each read
// is what came since the one before.
func TestKnocksPerWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 27, 14, 30, 0, 0, time.UTC)
	reads := []struct {
		at  time.Time
		ssh int64
	}{
		{time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), 100}, // this year
		{now.AddDate(0, 0, -3), 50},                        // this week
		{now.Add(-3 * time.Hour), 10},                      // today
		{now.Add(-30 * time.Minute), 20},                   // this hour
		{now.Add(-5 * time.Minute), 10},                    // the last 10 minutes
	}
	for _, read := range reads {
		if err := saveKnocks(dir, read.at, map[string]int64{"ssh": read.ssh, "postgres": 0}); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveKnocks(dir, time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC), map[string]int64{"postgres": 7}); err != nil {
		t.Fatal(err)
	}
	knocks, err := countKnocksAt(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(knocks.Names) != 2 || knocks.Names[0].Name != "ssh" {
		t.Fatalf("names = %+v", knocks.Names)
	}
	ssh := knocks.Names[0]
	want := KnockCount{Name: "ssh", Hour: 30, Today: 40, Week: 90, Year: 190, Total: 190, PerMinute: 1}
	if ssh != want {
		t.Fatalf("ssh = %+v, want %+v", ssh, want)
	}
	if knocks.All.Total != 197 || knocks.All.Year != 190 || knocks.All.Today != 40 {
		t.Fatalf("all = %+v", knocks.All)
	}
	if !knocks.Since.Equal(time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("since = %v", knocks.Since)
	}
}

func TestParseCounters(t *testing.T) {
	out := `{"nftables": [{"metainfo": {"version": "1.0.9"}}, {"counter": {"family": "inet", "name": "knock_ssh", "table": "moin", "handle": 1, "packets": 42, "bytes": 2520}}, {"counter": {"family": "inet", "name": "knock_node_red", "table": "moin", "handle": 2, "packets": 0, "bytes": 0}}]}`
	counters, err := parseCounters([]byte(out))
	if err != nil || counters["ssh"] != 42 || counters["node-red"] != 0 || len(counters) != 2 {
		t.Fatalf("counters = %v, %v", counters, err)
	}
}

// Minutes older than two days fold into one row per day, and count the
// same as before.
func TestKnocksFoldIntoDays(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 27, 14, 30, 0, 0, time.UTC)
	for _, at := range []time.Time{now.AddDate(0, 0, -5).Add(time.Minute), now.AddDate(0, 0, -5).Add(2 * time.Hour), now.Add(-time.Hour)} {
		if err := saveKnocks(dir, at, map[string]int64{"ssh": 3}); err != nil {
			t.Fatal(err)
		}
	}
	db, err := openKnocks(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM knocks`).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("rows = %d, %v, want one day and one minute", rows, err)
	}
	knocks, err := countKnocksAt(dir, now)
	if err != nil || knocks.All.Total != 9 || knocks.All.Week != 9 || knocks.All.Today != 3 {
		t.Fatalf("all = %+v, %v", knocks.All, err)
	}
}
