package agent

import (
	"context"
	"time"

	"github.com/axadrn/moin/protocol"
)

// healthEvery is how often the agent looks whether a backup broke or works
// again; failGrace is how long a failure lasts before it counts, so a
// database that restarts with a deploy sends no alert.
const (
	healthEvery = time.Minute
	failGrace   = 3 * time.Minute
)

// failure is a backup that does not work: since when, and why.
type failure struct {
	since time.Time
	why   string
}

// fail notes that a backup of the database in key broke, or with why ""
// that it works again. kind keeps the continuous backup and the snapshots
// apart, so one working does not clear the other.
func (b *backups) fail(key, kind, why string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures == nil {
		b.failures = map[string]failure{}
	}
	key += "\x00" + kind
	if why == "" {
		delete(b.failures, key)
		return
	}
	if old, ok := b.failures[key]; ok {
		old.why = why
		b.failures[key] = old
		return
	}
	b.failures[key] = failure{since: time.Now(), why: why}
}

// problem is why the backups of a database do not work right now, or "":
// a failure that lasts longer than failGrace, continuous backup first.
func (b *backups) problem(app string, database protocol.Database, now time.Time) string {
	key := drillKey(app, database)
	if database.Postgres() {
		state := b.walgState(key)
		b.walg.mu.Lock()
		broke := !state.broke.IsZero() && state.rebased.IsZero()
		why := state.why
		b.walg.mu.Unlock()
		if broke {
			// Lost its place in the log: it counts at once, a new base
			// backup is on its way.
			return "continuous backup broke: " + why + "; moin is taking a new base backup"
		}
	} else {
		b.mu.Lock()
		item, ok := b.watched[database.HostPath]
		b.mu.Unlock()
		if ok && item.db != nil {
			b.fail(key, "continuous", syncError(item.db.SyncDiagnostic().Error))
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, kind := range []string{"continuous", "snapshot"} {
		if failure, ok := b.failures[key+"\x00"+kind]; ok && now.Sub(failure.since) >= failGrace {
			return failure.why
		}
	}
	return ""
}

func syncError(err string) string {
	if err == "" {
		return ""
	}
	return "continuous backup: " + err
}

// health sends the status of a database whenever its backups or its
// restore check break or work again, so the control plane can alert at
// once instead of at the next daily check.
func (b *backups) health(ctx context.Context) {
	ticker := time.NewTicker(healthEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		b.healthOnce(ctx, time.Now())
	}
}

func (b *backups) healthOnce(ctx context.Context, now time.Time) {
	if _, err := b.ready(); err != nil {
		return
	}
	reports := map[string][]protocol.DatabaseBackup{}
	for app, databases := range b.allDatabases() {
		for _, database := range databases {
			if b.changed(app, database, now) {
				reports[app] = append(reports[app], b.status(ctx, app, database, false))
			}
		}
	}
	for app, databases := range reports {
		b.send(protocol.TypeBackupStatus, "", protocol.Backups{App: app, Databases: databases})
	}
}

// changed says whether the backup problem or the restore check of a
// database differs from what the agent last told: a healthy database it
// sees for the first time is no change.
func (b *backups) changed(app string, database protocol.Database, now time.Time) bool {
	key := drillKey(app, database)
	problem := b.problem(app, database, now)
	b.mu.Lock()
	defer b.mu.Unlock()
	state := problem + "\x00" + b.verified[key].err
	if b.reported == nil {
		b.reported = map[string]string{}
	}
	last, seen := b.reported[key]
	b.reported[key] = state
	if !seen {
		return state != "\x00"
	}
	return last != state
}

// allDatabases are the backed up databases of every app: the replicated
// SQLite files and the Postgres services.
func (b *backups) allDatabases() map[string][]protocol.Database {
	all := b.pgDatabases()
	if all == nil {
		all = map[string][]protocol.Database{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, item := range b.watched {
		all[item.app] = append(all[item.app], item.database)
	}
	return all
}
