package client

import (
	"context"
	"net/http"
	"time"

	"github.com/axadrn/ops/protocol"
)

// AppBackups are the backups of an app's databases.
type AppBackups struct {
	SnapshotBeforeChanges bool                      `json:"snapshot_before_changes"`
	Databases             []protocol.DatabaseBackup `json:"databases"`
}

// BackupStorage is the account's bucket, without its secret key.
type BackupStorage struct {
	Endpoint  string    `json:"endpoint"`
	Region    string    `json:"region"`
	Bucket    string    `json:"bucket"`
	Path      string    `json:"path"`
	Folder    string    `json:"folder,omitempty"`
	AccessKey string    `json:"access_key"`
	SecretKey string    `json:"secret_key,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Backups lists the snapshots and restore windows of an app.
func (c *Client) Backups(ctx context.Context, app string) (AppBackups, error) {
	var backups AppBackups
	err := c.do(ctx, http.MethodGet, appPath(app, "backups"), nil, &backups)
	return backups, err
}

// BackUp takes a snapshot of an app's databases now.
func (c *Client) BackUp(ctx context.Context, app string) (AppBackups, error) {
	var backups AppBackups
	err := c.do(ctx, http.MethodPost, appPath(app, "backups"), map[string]any{}, &backups)
	return backups, err
}

// Restore puts a database back from a snapshot or to a point in time.
func (c *Client) Restore(ctx context.Context, app, database, snapshot string, at *time.Time) error {
	body := map[string]any{"database": database, "snapshot": snapshot}
	if at != nil {
		body["at"] = at.UTC()
	}
	return c.do(ctx, http.MethodPost, appPath(app, "restore"), body, nil)
}

// SetSnapshotBeforeChanges turns the snapshot before every change on or off.
func (c *Client) SetSnapshotBeforeChanges(ctx context.Context, app string, on bool) error {
	return c.do(ctx, http.MethodPut, appPath(app, "backups/settings"), map[string]any{"snapshot_before_changes": on}, nil)
}

// BackupStorage returns the account's bucket.
func (c *Client) BackupStorage(ctx context.Context) (BackupStorage, error) {
	var storage BackupStorage
	err := c.do(ctx, http.MethodGet, "/api/v1/backup-storage", nil, &storage)
	return storage, err
}

// SetBackupStorage checks and stores the account's bucket.
func (c *Client) SetBackupStorage(ctx context.Context, storage BackupStorage) (BackupStorage, error) {
	// The API takes no created_at, so the bucket goes without it.
	body := struct {
		Endpoint  string `json:"endpoint"`
		Region    string `json:"region"`
		Bucket    string `json:"bucket"`
		Path      string `json:"path"`
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
	}{storage.Endpoint, storage.Region, storage.Bucket, storage.Path, storage.AccessKey, storage.SecretKey}
	var stored BackupStorage
	err := c.do(ctx, http.MethodPut, "/api/v1/backup-storage", body, &stored)
	return stored, err
}

// RemoveBackupStorage stops backups; the bucket keeps what is in it.
func (c *Client) RemoveBackupStorage(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/backup-storage", nil, nil)
}
