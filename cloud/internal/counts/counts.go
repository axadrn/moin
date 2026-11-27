// Package counts keeps the numbers moin cloud shows on the landing page:
// sums over everyone, by day in UTC. No number about one customer leaves
// it.
package counts

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/axadrn/moin/internal/report"
)

// The names of the counts.
const (
	Deploys = "deploys"
	Knocks  = "knocks"
)

// liveFor is how long a server counts as running after its last report.
const liveFor = 10 * time.Minute

// Counts are the counts in the database.
type Counts struct {
	db *sql.DB
}

// New keeps the counts in db.
func New(db *sql.DB) *Counts {
	return &Counts{db: db}
}

// dayOf is the day of t in UTC, as the table keeps it: 2026-11-27.
func dayOf(t time.Time) string {
	return t.UTC().Format(time.DateOnly)
}

// add adds n to the count name on the day of day.
func add(tx *sql.Tx, day time.Time, name string, n int64) error {
	if n <= 0 {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO counts (day, name, n) VALUES (?, ?, ?)
		ON CONFLICT (day, name) DO UPDATE SET n = n + excluded.n`, dayOf(day), name, n)
	return err
}

// Sum is the count name from the day of from to the day of to, both in.
func (c *Counts) Sum(name string, from, to time.Time) (int64, error) {
	var n int64
	err := c.db.QueryRow(`SELECT coalesce(sum(n), 0) FROM counts WHERE name = ? AND day >= ? AND day <= ?`,
		name, dayOf(from), dayOf(to)).Scan(&n)
	return n, err
}

// Period is one count this month, this year and in all.
type Period struct {
	Month int64 `json:"month"`
	Year  int64 `json:"year"`
	Total int64 `json:"total"`
}

// Totals are the numbers of the landing page: deploys and knocks over
// time, and the servers and apps that run now.
type Totals struct {
	Deploys Period `json:"deploys"`
	Knocks  Period `json:"knocks"`
	// Servers reported in the last 10 minutes; Apps are their apps.
	Servers int64 `json:"servers"`
	Apps    int64 `json:"apps"`
}

// Totals are the numbers of the landing page at now.
func (c *Counts) Totals(now time.Time) (Totals, error) {
	now = now.UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	year := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	var t Totals
	for _, count := range []struct {
		name   string
		period *Period
	}{{Deploys, &t.Deploys}, {Knocks, &t.Knocks}} {
		var err error
		if count.period.Month, err = c.Sum(count.name, month, now); err != nil {
			return t, err
		}
		if count.period.Year, err = c.Sum(count.name, year, now); err != nil {
			return t, err
		}
		if count.period.Total, err = c.Sum(count.name, time.Time{}, now); err != nil {
			return t, err
		}
	}
	err := c.db.QueryRow(`SELECT count(*), coalesce(sum(apps), 0) FROM servers WHERE last_report > ?`,
		now.Add(-liveFor).Unix()).Scan(&t.Servers, &t.Apps)
	return t, err
}

// Last is what the counts keep of a server's last report.
type Last struct {
	Releases map[string]int
	Knocks   int64
}

// Deltas are what a report adds to the counts after the report before:
// each increase of an app's release number is a deploy, and a new app
// counts its first release once. Knocks are the increase of the total;
// a lower total is a reset, and the new total counts. Before a server's
// first report every app is new and its knocks are all new.
func Deltas(before Last, r report.Report) (deploys, knocks int64, after Last) {
	after = Last{Releases: map[string]int{}}
	for _, app := range r.Apps {
		after.Releases[app.Name] = app.Release
		was, ok := before.Releases[app.Name]
		switch {
		case !ok && app.Release > 0:
			deploys++
		case app.Release > was:
			deploys += int64(app.Release - was)
		}
	}
	if r.Knocks != nil {
		after.Knocks = r.Knocks.Total
	} else {
		after.Knocks = before.Knocks
	}
	switch {
	case after.Knocks >= before.Knocks:
		knocks = after.Knocks - before.Knocks
	default:
		knocks = after.Knocks
	}
	return deploys, knocks, after
}

// Report counts the report of a server of a workspace, received at now,
// and keeps what it said for the next one.
func (c *Counts) Report(workspaceID, server string, r report.Report, now time.Time) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before Last
	var releases string
	err = tx.QueryRow(`SELECT releases, coalesce(knocks, 0) FROM servers WHERE workspace_id = ? AND name = ?`,
		workspaceID, server).Scan(&releases, &before.Knocks)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(releases), &before.Releases); err != nil {
		return err
	}
	deploys, knocked, after := Deltas(before, r)
	if err := add(tx, now, Deploys, deploys); err != nil {
		return err
	}
	if err := add(tx, now, Knocks, knocked); err != nil {
		return err
	}
	data, err := json.Marshal(after.Releases)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE servers SET last_report = ?, apps = ?, releases = ?, knocks = ? WHERE workspace_id = ? AND name = ?`,
		now.Unix(), len(r.Apps), string(data), after.Knocks, workspaceID, server)
	if err != nil {
		return err
	}
	return tx.Commit()
}
