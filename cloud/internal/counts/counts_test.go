package counts

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/cloud/internal/db"
	"github.com/axadrn/moin/internal/report"
)

func apps(releases map[string]int) []report.App {
	var out []report.App
	for name, release := range releases {
		out = append(out, report.App{Name: name, Release: release})
	}
	return out
}

func TestDeltas(t *testing.T) {
	knocks := func(total int64) *report.Knocks { return &report.Knocks{Total: total} }
	cases := []struct {
		name            string
		before          Last
		report          report.Report
		deploys, knocks int64
	}{
		{"first report: each app once, all knocks", Last{}, report.Report{Apps: apps(map[string]int{"shop": 7, "blog": 2}), Knocks: knocks(40)}, 2, 40},
		{"first report, an app never deployed", Last{}, report.Report{Apps: apps(map[string]int{"shop": 0})}, 0, 0},
		{"nothing new", Last{Releases: map[string]int{"shop": 7}, Knocks: 40}, report.Report{Apps: apps(map[string]int{"shop": 7}), Knocks: knocks(40)}, 0, 0},
		{"a jump counts each release", Last{Releases: map[string]int{"shop": 7}, Knocks: 40}, report.Report{Apps: apps(map[string]int{"shop": 10}), Knocks: knocks(45)}, 3, 5},
		{"a rollback is no deploy", Last{Releases: map[string]int{"shop": 7}}, report.Report{Apps: apps(map[string]int{"shop": 5})}, 0, 0},
		{"a new app counts once", Last{Releases: map[string]int{"shop": 7}}, report.Report{Apps: apps(map[string]int{"shop": 7, "blog": 4})}, 1, 0},
		{"a lower total is a reset", Last{Knocks: 900}, report.Report{Knocks: knocks(12)}, 0, 12},
		{"no knocks in the report keeps the total", Last{Knocks: 900}, report.Report{}, 0, 0},
	}
	for _, c := range cases {
		deploys, knocked, _ := Deltas(c.before, c.report)
		if deploys != c.deploys || knocked != c.knocks {
			t.Errorf("%s: deploys %d, knocks %d, want %d, %d", c.name, deploys, knocked, c.deploys, c.knocks)
		}
	}
	_, _, after := Deltas(Last{Knocks: 900}, report.Report{})
	if after.Knocks != 900 {
		t.Errorf("after a report without knocks: %d", after.Knocks)
	}
}

func TestReportAndTotals(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	c := New(database)
	if _, err := database.Exec(`INSERT INTO workspaces (id, mail, created) VALUES ('w1', 'ada@example.com', 0);
		INSERT INTO servers (id, workspace_id, name, token_hash, created) VALUES ('s1', 'w1', 'prod', 'h1', 0), ('s2', 'w1', 'web-1', 'h2', 0)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	lastYear := time.Date(now.Year()-1, 6, 1, 12, 0, 0, 0, time.UTC)
	first := report.Report{Server: "prod", Apps: apps(map[string]int{"shop": 3}), Knocks: &report.Knocks{Total: 100}}
	if err := c.Report("w1", "prod", first, lastYear); err != nil {
		t.Fatal(err)
	}
	second := report.Report{Server: "prod", Apps: apps(map[string]int{"shop": 5, "blog": 1}), Knocks: &report.Knocks{Total: 130}}
	if err := c.Report("w1", "prod", second, now); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(now, Installs, 4); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(now, Installs, 0); err != nil {
		t.Fatal(err)
	}
	totals, err := c.Totals(now)
	if err != nil {
		t.Fatal(err)
	}
	want := Totals{
		Deploys:  Period{Month: 3, Year: 3, Total: 4},
		Knocks:   Period{Month: 30, Year: 30, Total: 130},
		Installs: Period{Month: 4, Year: 4, Total: 4},
		Servers:  1, Apps: 2,
	}
	if totals != want {
		t.Errorf("totals = %+v, want %+v", totals, want)
	}
	if later, _ := c.Totals(now.Add(11 * time.Minute)); later.Servers != 0 || later.Apps != 0 {
		t.Errorf("a silent server still counts: %+v", later)
	}
}
