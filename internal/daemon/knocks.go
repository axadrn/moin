package daemon

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/report"
	_ "modernc.org/sqlite"
)

// A knock is a connection from the internet that moin's firewall drops.
// Bots try every new server within minutes: SSH, databases, admin pages.
// The firewall counts each knock under the name of its port, the daemon
// reads the counters every minute and keeps the counts in knocks.db.

// knockPorts are the ports knocks are counted by: report.KnockPorts, one
// place for the daemon and the site.
var knockPorts = report.KnockPorts

// knockOther counts the knocks on every other port.
const knockOther = report.KnockOther

// knockNames are the names of knockPorts, and other.
func knockNames() []string {
	names := []string{}
	for _, knock := range knockPorts {
		names = append(names, knock.Name)
	}
	return append(names, knockOther)
}

// knockCounter is the name of the firewall's counter for a knock name.
func knockCounter(name string) string { return `"knock-` + name + `"` }

const knocksFile = "knocks.db"

// countKnocks reads the firewall's counters every minute until ctx ends.
func countKnocks(ctx context.Context, dir string) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		counters, err := readCounters(ctx)
		if err != nil || len(counters) == 0 {
			continue
		}
		if err := saveKnocks(dir, time.Now(), counters); err != nil {
			slog.Error("count the knocks", "error", err)
		}
	}
}

// readCounters reads the knock counters of moin's table, by name. Without
// the table there are none.
func readCounters(ctx context.Context) (map[string]int64, error) {
	if !tableLoaded(ctx) {
		return nil, nil
	}
	out, err := exec.CommandContext(ctx, "nft", "-j", "list", "counters", "table", "inet", firewallTable).Output()
	if err != nil {
		return nil, err
	}
	return parseCounters(out)
}

// parseCounters reads what `nft -j list counters` prints: packets by
// knock name.
func parseCounters(out []byte) (map[string]int64, error) {
	var listed struct {
		Nftables []struct {
			Counter *struct {
				Name    string `json:"name"`
				Packets int64  `json:"packets"`
			} `json:"counter"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		return nil, err
	}
	counters := map[string]int64{}
	for _, item := range listed.Nftables {
		if item.Counter == nil {
			continue
		}
		if name, ok := strings.CutPrefix(item.Counter.Name, "knock-"); ok {
			counters[name] = item.Counter.Packets
		}
	}
	return counters, nil
}

// openKnocks opens knocks.db and makes its tables. knocks holds the
// count of each name per minute. last holds what each counter said the
// last time, for the next difference.
func openKnocks(dir string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, knocksFile)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS knocks (minute INTEGER, name TEXT, count INTEGER, PRIMARY KEY (minute, name));
CREATE TABLE IF NOT EXISTS last (name TEXT PRIMARY KEY, value INTEGER)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// knocksSince is how many knocks came since the last read. A counter
// lower than last time started again, after a reload or a reboot: all
// it holds is new. So is the first read.
func knocksSince(last, now int64, seen bool) int64 {
	if !seen || now < last {
		return now
	}
	return now - last
}

// saveKnocks keeps the knocks since the last read under the minute of
// at, and what each counter says now.
func saveKnocks(dir string, at time.Time, counters map[string]int64) error {
	db, err := openKnocks(dir)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	minute := at.Unix() / 60
	for name, now := range counters {
		var last int64
		err := tx.QueryRow(`SELECT value FROM last WHERE name = ?`, name).Scan(&last)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		count := knocksSince(last, now, err == nil)
		if count > 0 {
			_, err = tx.Exec(`INSERT INTO knocks (minute, name, count) VALUES (?, ?, ?)
ON CONFLICT (minute, name) DO UPDATE SET count = count + excluded.count`, minute, name, count)
			if err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO last (name, value) VALUES (?, ?)`, name, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Knocks are the knocks the firewall of this server dropped, all together
// and by name, the most first.
type Knocks struct {
	// Since is the minute of the first count, zero for none yet.
	Since time.Time    `json:"since,omitzero"`
	All   KnockCount   `json:"all"`
	Names []KnockCount `json:"names"`
}

// KnockCount is how many knocks one name had: in the last hour, today,
// the last 7 days, this year and since the first count. PerMinute is the
// rate of the last 10 minutes.
type KnockCount struct {
	Name      string  `json:"name"`
	Hour      int64   `json:"hour"`
	Today     int64   `json:"today"`
	Week      int64   `json:"week"`
	Year      int64   `json:"year"`
	Total     int64   `json:"total"`
	PerMinute float64 `json:"per_minute"`
}

// KnocksHere counts the knocks of this server up to now.
func KnocksHere(dir string) (Knocks, error) {
	return countKnocksAt(dir, time.Now())
}

// countKnocksAt counts the knocks up to now, with today and this year as
// the server's clock has them.
func countKnocksAt(dir string, now time.Time) (Knocks, error) {
	knocks := Knocks{All: KnockCount{Name: "all"}, Names: []KnockCount{}}
	db, err := openKnocks(dir)
	if err != nil {
		return knocks, err
	}
	defer db.Close()
	minute := func(t time.Time) int64 { return t.Unix() / 60 }
	year, month, day := now.Date()
	rows, err := db.Query(`SELECT name,
  SUM(CASE WHEN minute > ? THEN count ELSE 0 END),
  SUM(CASE WHEN minute >= ? THEN count ELSE 0 END),
  SUM(CASE WHEN minute > ? THEN count ELSE 0 END),
  SUM(CASE WHEN minute >= ? THEN count ELSE 0 END),
  SUM(count),
  SUM(CASE WHEN minute > ? THEN count ELSE 0 END),
  MIN(minute)
FROM knocks GROUP BY name`,
		minute(now.Add(-time.Hour)),
		minute(time.Date(year, month, day, 0, 0, 0, 0, now.Location())),
		minute(now.AddDate(0, 0, -7)),
		minute(time.Date(year, 1, 1, 0, 0, 0, 0, now.Location())),
		minute(now.Add(-10*time.Minute)))
	if err != nil {
		return knocks, err
	}
	defer rows.Close()
	for rows.Next() {
		var count KnockCount
		var lastTen, first int64
		if err := rows.Scan(&count.Name, &count.Hour, &count.Today, &count.Week, &count.Year, &count.Total, &lastTen, &first); err != nil {
			return knocks, err
		}
		count.PerMinute = float64(lastTen) / 10
		since := time.Unix(first*60, 0).UTC()
		if knocks.Since.IsZero() || since.Before(knocks.Since) {
			knocks.Since = since
		}
		knocks.Names = append(knocks.Names, count)
		all := &knocks.All
		all.Hour, all.Today, all.Week, all.Year, all.Total = all.Hour+count.Hour, all.Today+count.Today, all.Week+count.Week, all.Year+count.Year, all.Total+count.Total
		all.PerMinute += count.PerMinute
	}
	slices.SortFunc(knocks.Names, func(a, b KnockCount) int {
		return cmp.Or(cmp.Compare(b.Total, a.Total), cmp.Compare(a.Name, b.Name))
	})
	return knocks, rows.Err()
}

// knocksToday is the number of knocks today, for the status line. Zero
// when nothing counts.
func knocksToday(dir string) int64 {
	knocks, err := KnocksHere(dir)
	if err != nil {
		return 0
	}
	return knocks.All.Today
}

// knocksReport is the knocks in a report to the watchers, nil while the
// firewall counted none.
func knocksReport(dir string) *report.Knocks {
	knocks, err := KnocksHere(dir)
	if err != nil || knocks.All.Total == 0 {
		return nil
	}
	r := &report.Knocks{Today: knocks.All.Today, Year: knocks.All.Year, Total: knocks.All.Total, TodayByName: map[string]int64{}}
	for _, count := range knocks.Names {
		if count.Today > 0 {
			r.TodayByName[count.Name] = count.Today
		}
	}
	return r
}
