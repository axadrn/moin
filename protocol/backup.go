package protocol

import "time"

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
type DatabaseBackup struct {
	Database   string     `json:"database"`
	Kind       string     `json:"kind,omitempty"`
	CopiedAt   *time.Time `json:"copied_at,omitempty"`
	Copies     []Copy     `json:"copies,omitempty"`
	Undos      []Copy     `json:"undos,omitempty"`
	CheckedAt  *time.Time `json:"checked_at,omitempty"`
	CheckError string     `json:"check_error,omitempty"`
	// CheckedCopy is the copy the last check that worked restored.
	CheckedCopy string `json:"checked_copy,omitempty"`
	Error       string `json:"error,omitempty"`
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

// CopyFile is the copy id in its database's folder: a gzipped SQLite
// file, or a pg_dump in its custom format.
func CopyFile(id string, postgres bool) string {
	if postgres {
		return "copies/" + id + ".dump"
	}
	return "copies/" + id + ".db.gz"
}
