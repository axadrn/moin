package mcp

import (
	"context"
	"time"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/protocol"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerBackupTools registers the tools of an app's backups: listing,
// the schedule, restoring and downloading a copy.
func registerBackupTools(server *mcpsdk.Server, services Services) {
	tools := moinTools{services: services}
	mcpsdk.AddTool(server, newTool("get_backups", "List backups",
		"Lists how an app is backed up: the schedule (every_minutes, keep_days) and for every database its copies in the user's bucket, copied_at the newest, checked_at the newest daily check (check_error when it failed), problem in one short sentence when the backup does not work, and the moments a restore can go back to: the copies newest first, named before-v12 and the like when moin took one before a deploy, a data edit or a catalog update, and the state before each restore (undo-...). Needs the read scope.",
		readOnly("")), Read(member(services, tools.getBackups)))
	mcpsdk.AddTool(server, newTool("set_backups", "Set the backup schedule",
		"Sets how often moin copies the databases of an app into the user's bucket (every_minutes: 15, 60 or 1440), and how many days it keeps each copy (keep_days: 1, 3, 7 or 30). Returns the schedule. Needs a token with read and write access.",
		change(true, true)), Write(changes(services, tools.setBackups)))
	mcpsdk.AddTool(server, newTool("restore_app", "Restore a database",
		"Puts a database of an app back to a moment from get_backups, by its id or label, or to a time (RFC 3339): the newest copy at or before it. moin builds the restored database beside the live one and switches in one step; the state before stays as long as the app keeps copies, so the restore can be undone with its undo- moment. The app stops for the switch and starts again. It runs on its own and returns its state: running; unknown when the server went offline during it, until the server is back and says how it ended; then done, or failed with error. Call it again with id until it is done or failed; when it stays unknown, tell the user to check the app once the server is back. A moment two moments share as a label needs its id. Ask the user first: confirm must be the app's name, typed out. Needs a token with read and write access.",
		change(true, false)), Write(changes(services, tools.restoreApp)))
	mcpsdk.AddTool(server, newTool("download_backup", "Download a copy",
		"Returns link, a signed link to a copy of a database of an app in the app's own bucket, never through moin, valid 15 minutes: the newest copy, or the one moment names (a copy from get_backups). SQLite is gzipped, Postgres a pg_dump in its custom format. Needs a token with read and write access.",
		change(false, false)), Write(changes(services, tools.downloadBackup)))
}

// RestoreInput is the input of restore_app.
type RestoreInput struct {
	App      string  `json:"app" jsonschema:"the app name or ID"`
	Database string  `json:"database,omitempty" jsonschema:"the database's path in the container; needed when the app has several"`
	Moment   string  `json:"moment,omitempty" jsonschema:"the id or label of a moment from get_backups, like before-v12, 20261107T140000Z or undo-20261107T140200Z"`
	At       *string `json:"at,omitempty" jsonschema:"a time instead of a moment, RFC 3339: the newest copy at or before it"`
	Confirm  string  `json:"confirm" jsonschema:"the app's name, typed out, after the user agreed"`
	ID       string  `json:"id,omitempty" jsonschema:"the id of a restore this tool started, to see how it stands; then only app is needed"`
}

// RestoreOutput is the output of restore_app.
type RestoreOutput struct {
	api.Restore
}

// DownloadInput is the input of download_backup.
type DownloadInput struct {
	App      string `json:"app" jsonschema:"the app name or ID"`
	Database string `json:"database,omitempty" jsonschema:"the database's path in the container; needed when the app has several"`
	Moment   string `json:"moment,omitempty" jsonschema:"a copy from get_backups by id or label; the newest without one"`
}

// ScheduleInput is the input of set_backups.
type ScheduleInput struct {
	App          string `json:"app" jsonschema:"the app name or ID"`
	EveryMinutes int    `json:"every_minutes" jsonschema:"how often moin copies the databases: 15, 60 or 1440"`
	KeepDays     int    `json:"keep_days" jsonschema:"how many days each copy stays: 1, 3, 7 or 30"`
}

// ScheduleOutput is the output of set_backups.
type ScheduleOutput struct {
	api.BackupSchedule
}

// Backups is the output of get_backups.
type Backups struct {
	api.AppBackups
}

// DownloadOutput is the output of download_backup.
type DownloadOutput struct {
	api.Download
}

func (t moinTools) getBackups(ctx context.Context, caller Caller, input AppInput) (Backups, error) {
	app, backups, err := t.services.Apps.Backups(ctx, caller.Token.WorkspaceID, input.app())
	if err != nil {
		return Backups{}, err
	}
	return Backups{api.NewAppBackups(t.services.Apps, app, backups)}, nil
}

func (t moinTools) restoreApp(ctx context.Context, caller Caller, input RestoreInput) (RestoreOutput, error) {
	if input.ID != "" {
		restore, err := t.services.Apps.RestoreOf(caller.Token.WorkspaceID, input.App, input.ID)
		if err != nil {
			return RestoreOutput{}, err
		}
		return RestoreOutput{api.NewRestore(restore)}, nil
	}
	var at *time.Time
	if input.At != nil && *input.At != "" {
		parsed, err := time.Parse(time.RFC3339, *input.At)
		if err != nil {
			return RestoreOutput{}, InvalidField("at", "Use an RFC 3339 time like 2026-10-31T14:02:00Z.")
		}
		at = &parsed
	}
	app, err := t.services.Apps.Get(caller.Token.WorkspaceID, input.App)
	if err != nil {
		return RestoreOutput{}, err
	}
	// The service checks confirm against the app's name.
	restore, err := t.services.Apps.StartRestore(ctx, caller.actor(), app.ID, service.RestoreTarget{Database: input.Database, Moment: input.Moment, At: at, Confirm: input.Confirm})
	if err != nil {
		return RestoreOutput{}, err
	}
	// A small database is back in a moment: wait a little before answering.
	for deadline := time.Now().Add(20 * time.Second); restore.State == service.RestoreRunning && time.Now().Before(deadline); {
		select {
		case <-ctx.Done():
			return RestoreOutput{api.NewRestore(restore)}, nil
		case <-time.After(500 * time.Millisecond):
		}
		latest, err := t.services.Apps.RestoreOf(caller.Token.WorkspaceID, app.ID, restore.ID)
		if err != nil {
			return RestoreOutput{}, err
		}
		restore = latest
	}
	return RestoreOutput{api.NewRestore(restore)}, nil
}

func (t moinTools) setBackups(ctx context.Context, caller Caller, input ScheduleInput) (ScheduleOutput, error) {
	app, err := t.services.Apps.SetBackupSchedule(caller.actor(), input.App, protocol.Schedule{Every: input.EveryMinutes, Keep: input.KeepDays})
	if err != nil {
		return ScheduleOutput{}, err
	}
	return ScheduleOutput{api.NewBackupSchedule(app)}, nil
}

func (t moinTools) downloadBackup(ctx context.Context, caller Caller, input DownloadInput) (DownloadOutput, error) {
	download, err := t.services.Apps.Download(ctx, caller.actor(), input.App, input.Database, input.Moment)
	if err != nil {
		return DownloadOutput{}, err
	}
	return DownloadOutput{api.NewDownload(download)}, nil
}
