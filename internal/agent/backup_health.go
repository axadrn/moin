package agent

import (
	"context"
	"errors"
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

// urgent is a failure that counts at once, such as a history that lost
// its place and starts again.
type urgent struct{ error }

// fail notes that the backup of the database in key broke, or with why ""
// that it works again. An urgent failure counts at once.
func (b *backups) fail(key string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures == nil {
		b.failures = map[string]failure{}
	}
	if err == nil {
		delete(b.failures, key)
		return
	}
	since := time.Now()
	if errors.As(err, new(urgent)) {
		since = time.Time{}
	}
	if old, ok := b.failures[key]; ok {
		old.why = err.Error()
		if since.IsZero() {
			old.since = since
		}
		b.failures[key] = old
		return
	}
	b.failures[key] = failure{since: since, why: err.Error()}
}

// problem is why the backup of a database does not work right now, or "":
// a failure that lasts longer than failGrace.
func (b *backups) problem(app string, database protocol.Database, now time.Time) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if failure, ok := b.failures[key(app, database)]; ok && now.Sub(failure.since) >= failGrace {
		return failure.why
	}
	return ""
}

// health sends the status of a database whenever its backup or its daily
// check breaks or works again, so the control plane can alert at once
// instead of at the next daily check.
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
	for app, databases := range b.running() {
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

// changed says whether the backup problem or the daily check of a
// database differs from what the agent last told: a healthy database it
// sees for the first time is no change.
func (b *backups) changed(app string, database protocol.Database, now time.Time) bool {
	k := key(app, database)
	problem := b.problem(app, database, now)
	b.mu.Lock()
	defer b.mu.Unlock()
	state := problem + "\x00" + b.checked[k].err
	if b.reported == nil {
		b.reported = map[string]string{}
	}
	last, seen := b.reported[k]
	b.reported[k] = state
	if !seen {
		return state != "\x00"
	}
	return last != state
}
