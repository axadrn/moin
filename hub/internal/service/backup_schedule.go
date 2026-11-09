package service

// How often an app is copied and how long each copy stays.

import (
	"log/slog"
	"slices"
	"strconv"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/protocol"
)

// SetBackupSchedule sets how often the daemon copies an app's databases,
// how long it keeps each copy, and tells the daemon.
func (s *AppService) SetBackupSchedule(actor Actor, idOrName string, schedule protocol.Schedule) (*model.App, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(protocol.Everies, schedule.Every) {
		return nil, ErrBackupEvery
	}
	if !slices.Contains(protocol.Keeps, schedule.Keep) {
		return nil, ErrBackupKeep
	}
	if s.ScheduleInFile(app) {
		return nil, ErrScheduleInFile
	}
	if err := s.saveSchedule(actor, app, schedule); err != nil {
		return nil, err
	}
	return app, nil
}

// ScheduleInFile says the live deploy's compose.yml sets the backup
// schedule. Then only the file changes it.
func (s *AppService) ScheduleInFile(app *model.App) bool {
	return s.FileConfig(app).Backups != nil
}

// applyScheduleInFile saves the backup schedule compose.yml asks for.
func (s *AppService) applyScheduleInFile(app *model.App, backups protocol.ConfigBackups) {
	schedule, err := backups.Schedule()
	if err != nil {
		slog.Error("the backup schedule in compose.yml is wrong", "app_id", app.ID, "error", err.Error())
		return
	}
	if err := s.saveSchedule(System(app.WorkspaceID), app, schedule); err != nil {
		slog.Error("save the backup schedule failed", "app_id", app.ID, "error", err.Error())
	}
}

// saveSchedule stores a schedule, writes the change to the activity and
// tells the daemon.
func (s *AppService) saveSchedule(actor Actor, app *model.App, schedule protocol.Schedule) error {
	before := scheduleOf(app)
	if err := s.apps.SetBackupSchedule(app.ID, schedule.Every, schedule.Keep); err != nil {
		return err
	}
	app.BackupEvery, app.BackupKeep = schedule.Every, schedule.Keep
	if before != schedule {
		s.servers.record(actor, "backup.scheduled", "app", app.Name, map[string]string{
			"every": EveryText(before.Every) + " → " + EveryText(schedule.Every),
			"keep":  KeepText(before.Keep) + " → " + KeepText(schedule.Keep),
		})
	}
	if s.servers.Online(app.ServerID) {
		go s.configureDaemon(app.ServerID, app.WorkspaceID)
	}
	s.notify(app.WorkspaceID)
	return nil
}

// scheduleOf is the backup schedule of an app.
func scheduleOf(app *model.App) protocol.Schedule {
	return protocol.Schedule{Every: app.BackupEvery, Keep: app.BackupKeep}
}

// EveryText says how often, as people read it: "every 15 minutes",
// "hourly", "daily".
func EveryText(minutes int) string {
	switch minutes {
	case 60:
		return "hourly"
	case 24 * 60:
		return "daily"
	}
	return "every " + strconv.Itoa(minutes) + " minutes"
}

// KeepText says how long, as people read it: "1 day", "7 days".
func KeepText(days int) string {
	if days == 1 {
		return "1 day"
	}
	return strconv.Itoa(days) + " days"
}
