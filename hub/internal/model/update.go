package model

import "time"

// Release is a release of moin on GitHub.
type Release struct {
	Version     string
	URL         string
	PublishedAt time.Time
	// Summary is the "In short" line of the notes.
	Summary string
	// Notes are the release notes in Markdown, NotesHTML the same as HTML.
	Notes     string
	NotesHTML string
}

// ReadFirst is what the web, the CLI and MCP show above the release notes
// of a new release of a self-hosted hub.
const ReadFirst = "Read this first. Some updates need a step from you before you update."

// UpdateCommand updates a self-hosted hub, run on its server.
const UpdateCommand = "sh /opt/moin/update.sh"

// VersionInfo is the version of the hub and, on a self-hosted
// one, the newest release.
type VersionInfo struct {
	Version string
	// SelfHosted is false on the hosted hub, which updates with
	// every release and shows nothing of this.
	SelfHosted      bool
	Latest          string
	UpdateAvailable bool
	PublishedAt     *time.Time
	NotesURL        string
	Summary         string
	Notes           string
	NotesHTML       string
	CheckedAt       *time.Time
	// Admin is whether the user runs this hub.
	Admin bool
	// HubBackup is how the backup of the hub's own database stands, only
	// for the admins of the hub, on every hub.
	HubBackup *HubBackup
}

// HubBackup is how the backup of the hub's own database stands. Its
// settings stay in the env (S3_*, BACKUP_DATABASE): the place to restore
// from must not live in the database it restores.
type HubBackup struct {
	// State is on, off, or late when no copy reached the bucket for
	// HubBackupLateAfter.
	State    string     `json:"state"`
	Bucket   string     `json:"bucket"`
	LastCopy *time.Time `json:"last_copy"`
}

// HubBackupLateAfter is how long the bucket may go without a copy before
// the backup counts as late. Litestream checks in every second.
const HubBackupLateAfter = 2 * time.Minute

// Words of HubBackup.State.
const (
	HubBackupOn   = "on"
	HubBackupOff  = "off"
	HubBackupLate = "late"
)
