package model

import (
	"encoding/json"
	"time"
)

// App is a compose project on one server. The deploy fields come from its
// deploys: the newest one and the newest one that succeeded, which is live.
type App struct {
	ID             string    `db:"id"`
	WorkspaceID    string    `db:"workspace_id"`
	ServerID       string    `db:"server_id"`
	Name           string    `db:"name"`
	CreatedAt      time.Time `db:"created_at"`
	DeployHookHash *string   `db:"deploy_hook_hash"`
	// Domains is a JSON array; use DomainList.
	Domains   string `db:"domains"`
	Canonical string `db:"canonical"`
	// Protect is an htpasswd line when a password protects the app.
	Protect string `db:"protect"`
	// SnapshotBeforeChanges takes a snapshot before every change made
	// through ops, when backups are set up.
	SnapshotBeforeChanges bool `db:"snapshot_before_changes"`
	// Repo is owner/name on GitHub when the app follows one, with its
	// branch; AutoDeploy deploys every push to it.
	Repo           string `db:"repo"`
	Branch         string `db:"branch"`
	AutoDeploy     bool   `db:"auto_deploy"`
	InstallationID int64  `db:"installation_id"`
	// Stopped is set when its owner stopped the app. A deploy, a restart
	// or a start clears it.
	Stopped       bool   `db:"stopped"`
	ServerName    string `db:"server_name"`
	WorkspaceSlug string `db:"workspace_slug"`
	LatestNumber  int    `db:"latest_number"`
	LatestStatus  string `db:"latest_status"`
	LiveNumber    int    `db:"live_number"`
}

// Deploy statuses.
const (
	DeployRunning   = "running"
	DeploySucceeded = "succeeded"
	DeployFailed    = "failed"
)

// Deploy is one release of an app.
type Deploy struct {
	ID           string `db:"id"`
	AppID        string `db:"app_id"`
	Number       int    `db:"number"`
	Status       string `db:"status"`
	SourceKey    string `db:"source_key"`
	SourceSHA256 string `db:"source_sha256"`
	FromNumber   int    `db:"from_number"`
	Image        string `db:"image"`
	// Ref and CommitSHA name the GitHub commit a deploy came from.
	Ref           string     `db:"ref"`
	CommitSHA     string     `db:"commit_sha"`
	CommitMessage string     `db:"commit_message"`
	Error         string     `db:"error"`
	Log           string     `db:"log"`
	Detected      string     `db:"detected"`
	CreatedAt     time.Time  `db:"created_at"`
	FinishedAt    *time.Time `db:"finished_at"`
}

// Registry is a login for a container registry. Password is sealed.
type Registry struct {
	WorkspaceID string    `db:"workspace_id"`
	Host        string    `db:"host"`
	Username    string    `db:"username"`
	Password    string    `db:"password"`
	CreatedAt   time.Time `db:"created_at"`
}

// DomainList returns the app's domains.
func (a App) DomainList() []string {
	domains := []string{}
	_ = json.Unmarshal([]byte(a.Domains), &domains)
	return domains
}

// BackupStorage is the bucket an account's backups go to. SecretKey is
// sealed.
type BackupStorage struct {
	WorkspaceID string    `db:"workspace_id"`
	Endpoint    string    `db:"endpoint"`
	Region      string    `db:"region"`
	Bucket      string    `db:"bucket"`
	Path        string    `db:"path"`
	AccessKey   string    `db:"access_key"`
	SecretKey   string    `db:"secret_key"`
	CreatedAt   time.Time `db:"created_at"`
}

// BackupStatus is what the agent last reported about one database.
type BackupStatus struct {
	AppID        string     `db:"app_id"`
	Database     string     `db:"database"`
	Since        *time.Time `db:"since"`
	ReplicatedAt *time.Time `db:"replicated_at"`
	VerifiedAt   *time.Time `db:"verified_at"`
	Error        string     `db:"error"`
	UpdatedAt    time.Time  `db:"updated_at"`
}

// GitHubInstallation is the ops GitHub App on one account or organization.
type GitHubInstallation struct {
	InstallationID int64     `db:"installation_id"`
	WorkspaceID    string    `db:"workspace_id"`
	Account        string    `db:"account"`
	CreatedAt      time.Time `db:"created_at"`
	// SettingsURL is the page on github.com where its owner chooses the
	// repos ops may read; empty for installations stored before ops kept it.
	SettingsURL string `db:"settings_url"`
}
