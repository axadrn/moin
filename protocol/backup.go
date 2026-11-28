package protocol

import "time"

// How a restore stands on the server.
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
// says why it failed. Error says what keeps the backup from working, and
// BucketDown that it is the bucket, not this database. CloudError says
// why no watcher that takes copies took the last one of a server with a
// bucket, and CloudFailingSince since when none did.
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
	BucketDown  bool   `json:"bucket_down,omitempty"`

	CloudError        string     `json:"cloud_error,omitempty"`
	CloudFailingSince *time.Time `json:"cloud_failing_since,omitempty"`
}

// Copy is one copy of a database in the bucket, or one state before a
// restore on the server. Its ID starts with when it was taken
// (NewCopyID).
// Size is 0 when moin does not know it. Plain says a copy in the bucket is
// not encrypted: one from before every copy was.
type Copy struct {
	ID    string    `json:"id"`
	At    time.Time `json:"at"`
	Size  int64     `json:"size"`
	Plain bool      `json:"plain,omitempty"`
}

// BackupRestore puts one database back: to the copy Copy, or to the state
// Undo before an earlier restore, which undoes that restore. moin builds
// the database beside the live one, stops the app, switches in one step
// and starts it; the live one stays as the undo of this restore.
type BackupRestore struct {
	ID       string `json:"id"`
	App      string `json:"app"`
	Database string `json:"database,omitempty"`
	Copy     string `json:"copy,omitempty"`
	Undo     string `json:"undo,omitempty"`
}

// Encrypted ends the name of every copy in the bucket: an age file.
const Encrypted = ".age"

// CopyFile is the copy id in the folder of a database of kind: a gzipped
// SQLite file, a pg_dump in its custom format, a gzipped dump of MySQL or
// MariaDB, a gzipped archive of mongodump, or a zip of ClickHouse's
// BACKUP, encrypted with age. plain names a copy from before every copy
// was encrypted.
func CopyFile(id, kind string, plain bool) string {
	name := "copies/" + id + ".db.gz"
	switch kind {
	case DatabasePostgres:
		name = "copies/" + id + ".dump"
	case DatabaseMySQL:
		name = "copies/" + id + ".sql.gz"
	case DatabaseMongo:
		name = "copies/" + id + ".archive.gz"
	case DatabaseClickHouse:
		name = "copies/" + id + ".zip"
	}
	if plain {
		return name
	}
	return name + Encrypted
}
