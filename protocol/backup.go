package protocol

import (
	"path"
	"strings"
	"time"
)

// Bucket is an S3-compatible bucket. Everything of a workspace
// goes under <Path>/<Workspace>/, each app under <app>/<app ID>/:
//
//	<app>/<app ID>/uploads/                  sources of moin deploy, a day
//	<app>/<app ID>/<database>/copies/        the copies, as long as the app keeps them
//
// Workspace is the workspace's ID, which never changes, so workspaces
// sharing one bucket never touch each other's files. Name is the bucket's
// name in moin.
type Bucket struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Path      string `json:"path,omitempty"`
	Workspace string `json:"workspace"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Name      string `json:"name,omitempty"`
}

// BackupConfig is where backups go and how often. Bucket is the
// workspace's default bucket; without it the daemon copies nothing.
// AppBuckets is the bucket of each app, by name, that uses another one
// than the default. Apps are the IDs of the server's apps by name: an app
// deployed before deploys brought its ID gets it here. Schedules are the
// backup settings of each app by name; an app without one gets
// DefaultSchedule. The JSON keys storage and app_storage stay as they
// are: daemons of protocol version 1 read them.
type BackupConfig struct {
	Bucket     *Bucket             `json:"storage,omitempty"`
	AppBuckets map[string]*Bucket  `json:"app_storage,omitempty"`
	Apps       map[string]string   `json:"apps,omitempty"`
	Schedules  map[string]Schedule `json:"schedules,omitempty"`
	// Paused are the apps whose bucket the hub cannot open, with why:
	// the daemon copies and deletes nothing of them until it can.
	Paused map[string]string `json:"paused,omitempty"`
}

// Schedule is how an app is backed up: a copy every Every minutes, each
// kept Keep days.
type Schedule struct {
	Every int `json:"every_minutes"`
	Keep  int `json:"keep_days"`
}

// DefaultSchedule is the schedule of an app nobody set one for.
var DefaultSchedule = Schedule{Every: 60, Keep: 7}

// EveryMinutes is how often a word means, in minutes: 15m, 1h or 1d.
func EveryMinutes(word string) (int, bool) {
	minutes, ok := map[string]int{"15m": 15, "1h": 60, "1d": 24 * 60}[word]
	return minutes, ok
}

// KeepDays is how long a word means, in days: 1d, 3d, 7d or 30d.
func KeepDays(word string) (int, bool) {
	days, ok := map[string]int{"1d": 1, "3d": 3, "7d": 7, "30d": 30}[word]
	return days, ok
}

// Everies and Keeps are the choices of a Schedule: every 15 minutes,
// hourly or daily; kept 1, 3, 7 or 30 days.
var (
	Everies = []int{15, 60, 24 * 60}
	Keeps   = []int{1, 3, 7, 30}
)

// BackupRequest names an app; Database narrows it to one database.
type BackupRequest struct {
	App      string `json:"app"`
	Database string `json:"database,omitempty"`
}

// Backups are the backups of an app's databases.
type Backups struct {
	App       string           `json:"app"`
	Databases []DatabaseBackup `json:"databases"`
	// Restores are the restores of the app that run now, and how the
	// last ones ended, by the ID the hub gave them. A daemon
	// that reports restores sends a list, empty or not.
	Restores []RestoreState `json:"restores"`
	// Detected is set when the daemon found a new database in the live
	// deploy, long after the deploy ended.
	Detected *Detected `json:"detected,omitempty"`
}

// RestoreState is how a restore stands on the server: running, done or
// failed with Error.
type RestoreState struct {
	ID         string     `json:"id"`
	Database   string     `json:"database"`
	State      string     `json:"state"`
	Error      string     `json:"error,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// The states of a RestoreState.
const (
	RestoreRunning = "running"
	RestoreDone    = "done"
	RestoreFailed  = "failed"
)

// DatabaseBackup is how one database is backed up. Copies are its copies
// in the bucket, newest first; CopiedAt is the newest. Undos are the
// states before its restores, kept on the server: going back to one
// undoes that restore. CheckedAt is the newest daily check: it restored
// the newest copy into a throwaway database and read it, and CheckError
// says why it failed. Error says what keeps the backup from working.
// The JSON keys verified_at and drill_error stay as they are: daemons of
// protocol version 1 send them.
type DatabaseBackup struct {
	Database   string     `json:"database"`
	Kind       string     `json:"kind,omitempty"`
	CopiedAt   *time.Time `json:"copied_at,omitempty"`
	Copies     []Copy     `json:"copies,omitempty"`
	Undos      []Copy     `json:"undos,omitempty"`
	CheckedAt  *time.Time `json:"verified_at,omitempty"`
	CheckError string     `json:"drill_error,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// Copy is one copy of a database in the bucket, or one state before a
// restore on the server. Its ID starts with when it was taken
// (NewCopyID).
// Size is 0 when moin does not know it.
type Copy struct {
	ID   string    `json:"id"`
	At   time.Time `json:"at"`
	Size int64     `json:"size"`
}

// BackupRestore puts one database back: to the copy Copy, or to the state
// Undo before an earlier restore, which undoes that restore. The daemon builds the database
// beside the live one, stops the app, switches in one step and starts it;
// the live one stays as the undo of this restore.
type BackupRestore struct {
	ID       string `json:"id"`
	App      string `json:"app"`
	Database string `json:"database,omitempty"`
	Copy     string `json:"copy,omitempty"`
	Undo     string `json:"undo,omitempty"`
}

// DatabaseFolder is the folder of a database in its app's bucket:
// <path>/<workspace>/<app>/<app ID>/<database>, its path in the container
// with ~ for /, like data~app.db, postgres-db or postgres-db~analytics.
func DatabaseFolder(bucket Bucket, app, appID, database string) string {
	// Each / becomes ~, so /data/app.db and /data-app.db never share one.
	name := strings.NewReplacer("%", "%25", "~", "%7E", "/", "~").Replace(strings.Trim(database, "/"))
	return strings.Trim(path.Join(bucket.Path, bucket.Workspace, app, appID, name), "/")
}

// CopyFile is the copy id in its database's folder: a gzipped SQLite
// file, or a pg_dump in its custom format.
func CopyFile(id string, postgres bool) string {
	if postgres {
		return "copies/" + id + ".dump"
	}
	return "copies/" + id + ".db.gz"
}
