package service

// The moments of a database: what a person can go back to.

import (
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

// DatabaseBackups is the backup of one database as people see it: what
// the daemon reports, Problem as one short sentence when it does not work,
// and the moments a restore can go back to.
type DatabaseBackups struct {
	protocol.DatabaseBackup
	Problem string
	Moments []Moment
}

// Moment is one moment of a database to go back to: a minute a few
// minutes or hours ago (SQLite), a copy (Postgres), named "Before v12"
// when it was before a change (a plain time for SQLite), or the state
// before a restore, which undoes it. ID is what the CLI, the API and MCP
// take; Copy and Undo name what a restore goes back to; without both it
// goes back to the time At.
type Moment struct {
	ID    string
	Label string
	At    time.Time
	Kind  string
	Copy  string
	Undo  string
}

// Kinds of a Moment, besides the kinds of a remembered moment
// (model.MomentDeploy, MomentUpdate, MomentEdit).
const (
	MomentMinute = "minute"
	MomentCopy   = "copy"
	MomentUndo   = "undo"
)

// quickMoments are the minutes a few minutes and hours back.
var quickMoments = []struct {
	id, label string
	ago       time.Duration
}{
	{"1m", "1 minute ago", time.Minute},
	{"15m", "15 minutes ago", 15 * time.Minute},
	{"1h", "1 hour ago", time.Hour},
	{"1d", "Yesterday this time", 24 * time.Hour},
}

// keepFor is how long an app keeps its backups.
func keepFor(app *model.App) time.Duration {
	if app.BackupKeep <= 0 {
		return time.Duration(protocol.DefaultSchedule.Keep) * 24 * time.Hour
	}
	return time.Duration(app.BackupKeep) * 24 * time.Hour
}

// everyChangeReach is how far Every change reaches: from its oldest
// moment to now while it works, else to its newest change.
func everyChangeReach(backup protocol.DatabaseBackup, now time.Time) (since, until time.Time, ok bool) {
	if backup.Since == nil {
		return since, until, false
	}
	since, until = *backup.Since, now
	if backup.Error != "" && backup.Until != nil {
		until = *backup.Until
	}
	return since, until, true
}

// Moments are the moments a database can go back to: the quick minutes
// and the remembered times Every change reaches (SQLite), the copies
// (Postgres), and the undos, newest first. A copy moin took before a
// change is named after it, and so is a time.
func Moments(backup protocol.DatabaseBackup, remembered []model.BackupMoment, now time.Time) []Moment {
	moments := []Moment{}
	later := []Moment{}
	if since, until, ok := everyChangeReach(backup, now); ok {
		for _, quick := range quickMoments {
			if at := now.Add(-quick.ago); !at.Before(since) && !at.After(until) {
				moments = append(moments, Moment{ID: quick.id, Label: quick.label, At: at, Kind: MomentMinute})
			}
		}
		for _, named := range remembered {
			if (named.Database == "" || named.Database == backup.Database) && !named.At.Before(since) && !named.At.After(until) {
				id, label, kind := rememberedMoment(named)
				later = append(later, Moment{ID: id, Label: label, At: named.At, Kind: kind})
			}
		}
	}
	for _, copy := range backup.Copies {
		moment := Moment{ID: copy.ID, Label: atWhen(copy.At, now), At: copy.At, Kind: MomentCopy, Copy: copy.ID}
		for _, named := range remembered {
			// The copy before a change by its full ID; old ones by the time.
			if (copy.ID == protocol.BeforeCopyID(named.At) || copy.ID == named.At.UTC().Format(protocol.CopyIDLayout)) && (named.Database == "" || named.Database == backup.Database) {
				moment.ID, moment.Label, moment.Kind = rememberedMoment(named)
			}
		}
		later = append(later, moment)
	}
	for _, undo := range backup.Undos {
		when := atWhen(undo.At, now)
		if after, today := strings.CutPrefix(when, "At "); today {
			when = "at " + after
		} else {
			when = "on " + when
		}
		later = append(later, Moment{ID: "undo-" + undo.ID, Label: "Before the restore " + when, At: undo.At, Kind: MomentUndo, Undo: undo.ID})
	}
	slices.SortStableFunc(later, func(x, y Moment) int { return y.At.Compare(x.At) })
	return append(moments, later...)
}

// rememberedMoment names a moment before a change.
func rememberedMoment(moment model.BackupMoment) (id, label, kind string) {
	switch moment.Kind {
	case model.MomentDeploy:
		return "before-v" + moment.Detail, "Before v" + moment.Detail, moment.Kind
	case model.MomentUpdate:
		number, version, _ := strings.Cut(moment.Detail, " ")
		return "before-v" + number, "Before the update to " + version, moment.Kind
	}
	what, table, _ := strings.Cut(moment.Detail, ":")
	id = "edit-" + strconv.FormatInt(moment.ID, 10)
	switch what {
	case "cell":
		return id, "Before your edit in " + table, moment.Kind
	case "add":
		return id, "Before the row added to " + table, moment.Kind
	case "delete":
		return id, "Before the row deleted from " + table, moment.Kind
	}
	return id, "Before your SQL change", moment.Kind
}

// atWhen says when a copy was, in UTC: "At 15:04" today, else "Nov 6 at
// 15:04".
func atWhen(at, now time.Time) string {
	at, now = at.UTC(), now.UTC()
	if at.YearDay() == now.YearDay() && at.Year() == now.Year() {
		return "At " + at.Format("15:04")
	}
	return at.Format("Jan 2") + " at " + at.Format("15:04")
}

// findMoment is the moment named by its ID, or by its label when no other
// moment has the same one, in any case.
func findMoment(moments []Moment, name string) (Moment, error) {
	name = strings.TrimSpace(name)
	var found []Moment
	for _, moment := range moments {
		if strings.EqualFold(moment.ID, name) {
			return moment, nil
		}
		if strings.EqualFold(moment.Label, name) {
			found = append(found, moment)
		}
	}
	switch len(found) {
	case 0:
		return Moment{}, ErrMoment
	case 1:
		return found[0], nil
	}
	ids := make([]string, 0, len(found))
	for _, moment := range found {
		ids = append(ids, moment.ID)
	}
	return Moment{}, &AmbiguousMomentError{Label: found[0].Label, IDs: ids}
}

// AmbiguousMomentError refuses a label several moments have.
type AmbiguousMomentError struct {
	Label string
	IDs   []string
}

func (e *AmbiguousMomentError) Error() string {
	return "several moments are called " + e.Label + ": name one by its id, " + strings.Join(e.IDs, " or ")
}

// remember names the moment before a change, by its time, and forgets the
// names of moments older than the app keeps.
func (s *AppService) remember(app *model.App, database, kind, detail string, at time.Time) {
	err := s.apps.AddBackupMoment(model.BackupMoment{AppID: app.ID, Database: database, Kind: kind, Detail: detail, At: at.UTC()}, s.now().Add(-keepFor(app)-24*time.Hour))
	if err != nil {
		slog.Error("remember a moment failed", "app_id", app.ID, "kind", kind, "error", err)
	}
}

// markUpdate says a deploy updates a catalog app to version, so the moment
// before it reads "Before the update to 3.4.1".
func (s *AppService) markUpdate(deployID, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updates == nil {
		s.updates = map[string]string{}
	}
	s.updates[deployID] = version
}

// rememberDeploy names the moment before a deploy.
func (s *AppService) rememberDeploy(deployID string, done protocol.DeployDone) {
	s.mu.Lock()
	version, update := s.updates[deployID]
	delete(s.updates, deployID)
	s.mu.Unlock()
	if done.Before == nil {
		return
	}
	deploy, err := s.apps.DeployByID(deployID)
	if err != nil {
		return
	}
	app, err := s.apps.ByID(deploy.AppID)
	if err != nil {
		return
	}
	number := strconv.Itoa(deploy.Number)
	if update {
		s.remember(app, "", model.MomentUpdate, number+" "+version, *done.Before)
		return
	}
	s.remember(app, "", model.MomentDeploy, number, *done.Before)
}
