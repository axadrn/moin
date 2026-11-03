package api

import (
	"net/http"
	"time"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/protocol"
)

// BackupStorage is the account's bucket, without its secret key. Folder
// is where this workspace's files go in it: path, then the workspace ID.
type BackupStorage struct {
	Endpoint  string    `json:"endpoint"`
	Region    string    `json:"region"`
	Bucket    string    `json:"bucket"`
	Path      string    `json:"path"`
	Folder    string    `json:"folder"`
	AccessKey string    `json:"access_key"`
	CreatedAt time.Time `json:"created_at"`
}

// BackupStorageRequest is the body of PUT /api/v1/backup-storage.
type BackupStorageRequest struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Path      string `json:"path"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

// AppBackups is the answer of GET and POST /api/v1/apps/{app}/backups.
// Bucket is the bucket new backups go to; OlderBuckets are the buckets the
// app left, which keep its older backups. Each snapshot names its bucket.
type AppBackups struct {
	SnapshotBeforeChanges bool                      `json:"snapshot_before_changes"`
	Bucket                string                    `json:"bucket"`
	OlderBuckets          []string                  `json:"older_buckets"`
	Databases             []protocol.DatabaseBackup `json:"databases"`
}

// NewAppBackups is the JSON shape of an app's backups.
func NewAppBackups(apps *service.AppService, app *model.App, backups protocol.Backups) AppBackups {
	answer := AppBackups{SnapshotBeforeChanges: app.SnapshotBeforeChanges, OlderBuckets: []string{}, Databases: backups.Databases}
	if bucket, err := apps.AppBucket(app); err == nil {
		answer.Bucket = bucket.Name
	}
	if older, err := apps.OlderBuckets(app); err == nil {
		for _, bucket := range older {
			answer.OlderBuckets = append(answer.OlderBuckets, bucket.Name)
		}
	}
	return answer
}

// BackupSettings is the body of PUT /api/v1/apps/{app}/backups/settings.
type BackupSettings struct {
	SnapshotBeforeChanges bool `json:"snapshot_before_changes"`
}

// RestoreRequest is the body of POST /api/v1/apps/{app}/restore: a
// snapshot ID, or a point in time. Bucket is the bucket the snapshot is in
// when the app left it; empty is the app's bucket.
type RestoreRequest struct {
	Database string     `json:"database"`
	Snapshot string     `json:"snapshot"`
	Bucket   string     `json:"bucket"`
	At       *time.Time `json:"at"`
}

func newBackupStorage(storage *service.Bucket) BackupStorage {
	return BackupStorage{Endpoint: storage.Endpoint, Region: storage.Region, Bucket: storage.Bucket, Path: storage.Path, Folder: storage.Folder, AccessKey: storage.AccessKey, CreatedAt: storage.CreatedAt}
}

// GetBackupStorage returns the account's bucket.
func (h *AppsHandler) GetBackupStorage(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	storage, err := h.apps.BackupStorage(workspace.ID)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, newBackupStorage(storage))
}

// SetBackupStorage checks and stores the account's bucket.
func (h *AppsHandler) SetBackupStorage(w http.ResponseWriter, r *http.Request) {
	var request BackupStorageRequest
	if !Decode(w, r, &request) {
		return
	}
	err := h.apps.SetBackupStorage(r.Context(), actor(r), service.BackupStorageInput(request))
	if err != nil {
		ServiceError(w, err)
		return
	}
	h.GetBackupStorage(w, r)
}

// DeleteBackupStorage forgets the bucket.
func (h *AppsHandler) DeleteBackupStorage(w http.ResponseWriter, r *http.Request) {
	err := h.apps.RemoveBackupStorage(actor(r))
	if err != nil {
		ServiceError(w, err)
		return
	}
	NoContent(w)
}

// Backups lists the snapshots and restore windows of an app's databases.
func (h *AppsHandler) Backups(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, backups, err := h.apps.Backups(r.Context(), workspace.ID, r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewAppBackups(h.apps, app, backups))
}

// BackUp takes a snapshot now.
func (h *AppsHandler) BackUp(w http.ResponseWriter, r *http.Request) {
	app, backups, err := h.apps.BackUp(r.Context(), actor(r), r.PathValue("app"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusCreated, NewAppBackups(h.apps, app, backups))
}

// SetBackupSettings turns the snapshot before every change on or off.
func (h *AppsHandler) SetBackupSettings(w http.ResponseWriter, r *http.Request) {
	var request BackupSettings
	if !Decode(w, r, &request) {
		return
	}
	app, err := h.apps.SetSnapshotBeforeChanges(actor(r), r.PathValue("app"), request.SnapshotBeforeChanges)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, BackupSettings{SnapshotBeforeChanges: app.SnapshotBeforeChanges})
}

// Restore puts a database back from a snapshot or to a point in time.
func (h *AppsHandler) Restore(w http.ResponseWriter, r *http.Request) {
	var request RestoreRequest
	if !Decode(w, r, &request) {
		return
	}
	err := h.apps.Restore(r.Context(), actor(r), r.PathValue("app"), request.Database, request.Snapshot, request.Bucket, request.At)
	if err != nil {
		ServiceError(w, err)
		return
	}
	NoContent(w)
}
