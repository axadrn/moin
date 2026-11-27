package daemon

import (
	"testing"
	"time"
)

// A counter that went down started again: all it holds is new. So is the
// first read.
func TestKnocksSince(t *testing.T) {
	cases := []struct {
		last, now int64
		seen      bool
		want      int64
	}{
		{0, 12, false, 12}, // the first read
		{10, 15, true, 5},  // five more
		{15, 15, true, 0},  // none
		{500, 3, true, 3},  // a reload or a reboot set it back
	}
	for _, c := range cases {
		if got := knocksSince(c.last, c.now, c.seen); got != c.want {
			t.Errorf("knocksSince(%d, %d, %v) = %d, want %d", c.last, c.now, c.seen, got, c.want)
		}
	}
}

// Each window counts what it should, from reads across a year.
func TestKnocksPerWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 27, 14, 30, 0, 0, time.UTC)
	reads := []struct {
		at  time.Time
		ssh int64
	}{
		{time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), 100}, // the first read: 100, this year
		{now.AddDate(0, 0, -3), 150},                       // 50, this week
		{now.Add(-3 * time.Hour), 10},                      // reset: 10, today
		{now.Add(-30 * time.Minute), 30},                   // 20, this hour
		{now.Add(-5 * time.Minute), 40},                    // 10, the last 10 minutes
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
	out := `{"nftables": [{"metainfo": {"version": "1.0.9"}}, {"counter": {"family": "inet", "name": "knock-ssh", "table": "moin", "handle": 1, "packets": 42, "bytes": 2520}}, {"counter": {"family": "inet", "name": "knock-node-red", "table": "moin", "handle": 2, "packets": 0, "bytes": 0}}]}`
	counters, err := parseCounters([]byte(out))
	if err != nil || counters["ssh"] != 42 || len(counters) != 2 {
		t.Fatalf("counters = %v, %v", counters, err)
	}
}
