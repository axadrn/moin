package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/protocol"
)

// BackupSchedule is how an app is backed up: a copy every every_minutes
// (15, 60 or 1440), each kept keep_days (1, 3, 7 or 30); every_change
// also records every change of a SQLite file. PUT
// /api/v1/apps/{app}/backups takes it.
type BackupSchedule struct {
	EveryMinutes int  `json:"every_minutes"`
	KeepDays     int  `json:"keep_days"`
	EveryChange  bool `json:"every_change"`
}

// NewBackupSchedule is the JSON shape of an app's schedule.
func NewBackupSchedule(app *model.App) BackupSchedule {
	return BackupSchedule{EveryMinutes: app.BackupEvery, KeepDays: app.BackupKeep, EveryChange: app.BackupEveryChange}
}

// AppBackups is the answer of GET /api/v1/apps/{app}/backups: the bucket
// the copies go to, the user's own, the schedule and each database.
type AppBackups struct {
	Bucket string `json:"bucket"`
	BackupSchedule
	Databases []DatabaseBackups `json:"databases"`
}

// DatabaseBackup is how one database is backed up, as the daemon reports
// it: protocol.DatabaseBackup under the API's own names. since and until
// are how far Every change reaches.
type DatabaseBackup struct {
	Database   string          `json:"database"`
	Kind       string          `json:"kind,omitempty"`
	CopiedAt   *time.Time      `json:"copied_at,omitempty"`
	CheckedAt  *time.Time      `json:"checked_at,omitempty"`
	CheckError string          `json:"check_error,omitempty"`
	Since      *time.Time      `json:"since,omitempty"`
	Until      *time.Time      `json:"until,omitempty"`
	Copies     []protocol.Copy `json:"copies"`
	Undos      []protocol.Copy `json:"undos"`
	Error      string          `json:"error,omitempty"`
}

// DatabaseBackups is the backup of one database: what the daemon reports,
// problem as one short sentence when it does not work, and the moments a
// restore can go back to, the quick minutes first, then the copies and
// the undos, newest first.
type DatabaseBackups struct {
	DatabaseBackup
	Problem string   `json:"problem,omitempty"`
	Moments []Moment `json:"moments"`
}

// Moment is one moment to go back to. POST /api/v1/apps/{app}/restores
// takes its id or label as moment.
type Moment struct {
	ID    string    `json:"id"`
	Label string    `json:"label"`
	At    time.Time `json:"at"`
	Kind  string    `json:"kind"`
}

// NewAppBackups is the JSON shape of an app's backups.
func NewAppBackups(apps *service.AppService, app *model.App, databases []service.DatabaseBackups) AppBackups {
	answer := AppBackups{BackupSchedule: NewBackupSchedule(app), Databases: []DatabaseBackups{}}
	if bucket, err := apps.AppBucket(app); err == nil {
		answer.Bucket = bucket.Name
	}
	for _, database := range databases {
		backup := database.DatabaseBackup
		shown := DatabaseBackups{DatabaseBackup: DatabaseBackup{
			Database: backup.Database, Kind: backup.Kind, CopiedAt: backup.CopiedAt, CheckedAt: backup.CheckedAt, CheckError: backup.CheckError,
			Since: backup.Since, Until: backup.Until, Copies: backup.Copies, Undos: backup.Undos, Error: backup.Error,
		}, Problem: database.Problem, Moments: []Moment{}}
		if shown.Copies == nil {
			shown.Copies = []protocol.Copy{}
		}
		if shown.Undos == nil {
			shown.Undos = []protocol.Copy{}
		}
		for _, moment := range database.Moments {
			shown.Moments = append(shown.Moments, Moment{ID: moment.ID, Label: moment.Label, At: moment.At, Kind: moment.Kind})
		}
		answer.Databases = append(answer.Databases, shown)
	}
	return answer
}

// RestoreRequest is the body of POST /api/v1/apps/{app}/restores: a
// moment from the list, by id or label, or a time. Database names the
// database when the app has several. Confirm is the app's name, typed
// out by the user.
type RestoreRequest struct {
	Database string     `json:"database"`
	Moment   string     `json:"moment"`
	At       *time.Time `json:"at"`
	Confirm  string     `json:"confirm"`
}

// DownloadRequest is the body of POST /api/v1/apps/{app}/backups/download:
// the database when the app has several, and a moment from the list, the
// newest copy without one.
type DownloadRequest struct {
	Database string `json:"database"`
	Moment   string `json:"moment"`
}

// Download is a copy of a database in the app's own bucket; link is a
// signed link to it there, valid 15 minutes.
type Download struct {
	App      string    `json:"app"`
	Database string    `json:"database"`
	Copy     string    `json:"copy"`
	Label    string    `json:"label"`
	At       time.Time `json:"at"`
	Size     int64     `json:"size"`
	Link     string    `json:"link"`
}

// NewDownload is the JSON shape of a download.
func NewDownload(download *service.Download) Download {
	return Download{App: download.App, Database: download.Database, Copy: download.Copy, Label: download.Label, At: download.At, Size: download.Size, Link: download.Link}
}

// Backups lists the backups of an app's databases with their moments.
func (h *AppsHandler) Backups(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, databases, err := h.apps.Backups(r.Context(), workspace.ID, r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewAppBackups(h.apps, app, databases))
}

// SetBackupSchedule sets how often an app is copied and how long each
// copy stays.
func (h *AppsHandler) SetBackupSchedule(w http.ResponseWriter, r *http.Request) {
	var request BackupSchedule
	if !Decode(w, r, &request) {
		return
	}
	app, err := h.apps.SetBackupSchedule(actor(r), r.PathValue("app"), protocol.Schedule{Every: request.EveryMinutes, Keep: request.KeepDays, EveryChange: request.EveryChange})
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewBackupSchedule(app))
}

// Restore is a restore the daemon runs: running, then done or failed with
// error. To is the moment it goes back to, as people read it.
type Restore struct {
	ID         string     `json:"id"`
	App        string     `json:"app"`
	Database   string     `json:"database"`
	To         string     `json:"to"`
	State      string     `json:"state"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// NewRestore is the JSON shape of a restore.
func NewRestore(restore *service.Restore) Restore {
	return Restore{ID: restore.ID, App: restore.App, Database: restore.Database, To: restore.To, State: restore.State, Error: restore.Error, StartedAt: restore.StartedAt, FinishedAt: restore.FinishedAt}
}

// StartRestore checks the moment and has the daemon put the database back
// to it, on its own: 202 with the restore to follow.
func (h *AppsHandler) StartRestore(w http.ResponseWriter, r *http.Request) {
	var request RestoreRequest
	if !Decode(w, r, &request) {
		return
	}
	restore, err := h.apps.StartRestore(r.Context(), actor(r), r.PathValue("app"), service.RestoreTarget(request))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusAccepted, NewRestore(restore))
}

// Restore says how a restore stands.
func (h *AppsHandler) Restore(w http.ResponseWriter, r *http.Request) {
	restore, err := h.apps.RestoreOf(ctxkeys.Workspace(r.Context()).ID, r.PathValue("app"), r.PathValue("restore"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewRestore(restore))
}

// Download signs a link to a copy of a database in the app's bucket.
func (h *AppsHandler) Download(w http.ResponseWriter, r *http.Request) {
	var request DownloadRequest
	if !Decode(w, r, &request) {
		return
	}
	download, err := h.apps.Download(r.Context(), actor(r), r.PathValue("app"), request.Database, request.Moment)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewDownload(download))
}
