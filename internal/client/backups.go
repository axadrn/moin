package client

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/axadrn/ops/protocol"
)

// AppBackups are the backups of an app's databases. Bucket is where new
// backups go; OlderBuckets keep the older ones, which are listed too.
type AppBackups struct {
	SnapshotBeforeChanges bool                      `json:"snapshot_before_changes"`
	Bucket                string                    `json:"bucket"`
	OlderBuckets          []string                  `json:"older_buckets"`
	Databases             []protocol.DatabaseBackup `json:"databases"`
}

// Bucket is one bucket of the workspace, without its secret key. Apps use
// it now; OlderBackups moved away and keep older backups there.
type Bucket struct {
	Name         string    `json:"name"`
	Default      bool      `json:"default"`
	Endpoint     string    `json:"endpoint"`
	Region       string    `json:"region"`
	Bucket       string    `json:"bucket"`
	Path         string    `json:"path"`
	Folder       string    `json:"folder,omitempty"`
	AccessKey    string    `json:"access_key"`
	Apps         []string  `json:"apps,omitempty"`
	OlderBackups []string  `json:"older_backups,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// BucketInput adds or changes a bucket. On a change an empty Name keeps
// the name and an empty SecretKey the stored key.
type BucketInput struct {
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Path      string `json:"path"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Default   bool   `json:"default"`
}

// Input is the bucket as it is, to change some of it.
func (b Bucket) Input() BucketInput {
	return BucketInput{Name: b.Name, Endpoint: b.Endpoint, Region: b.Region, Bucket: b.Bucket, Path: b.Path, AccessKey: b.AccessKey, Default: b.Default}
}

// Buckets lists the workspace's buckets, the default first.
func (c *Client) Buckets(ctx context.Context) ([]Bucket, error) {
	var list struct {
		Items []Bucket `json:"items"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/buckets", nil, &list)
	return list.Items, err
}

// Bucket returns one bucket by name.
func (c *Client) Bucket(ctx context.Context, name string) (Bucket, error) {
	var bucket Bucket
	err := c.do(ctx, http.MethodGet, "/api/v1/buckets/"+url.PathEscape(name), nil, &bucket)
	return bucket, err
}

// AddBucket checks and stores a new bucket.
func (c *Client) AddBucket(ctx context.Context, input BucketInput) (Bucket, error) {
	var bucket Bucket
	err := c.do(ctx, http.MethodPost, "/api/v1/buckets", input, &bucket)
	return bucket, err
}

// EditBucket changes a bucket.
func (c *Client) EditBucket(ctx context.Context, name string, input BucketInput) (Bucket, error) {
	var bucket Bucket
	err := c.do(ctx, http.MethodPut, "/api/v1/buckets/"+url.PathEscape(name), input, &bucket)
	return bucket, err
}

// RemoveBucket forgets a bucket nothing uses; what is in it stays.
func (c *Client) RemoveBucket(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/buckets/"+url.PathEscape(name), nil, nil)
}

// SetAppBucket moves an app's uploads and new backups to a bucket; empty
// follows the workspace's default.
func (c *Client) SetAppBucket(ctx context.Context, app, bucket string) (App, error) {
	var answer App
	err := c.do(ctx, http.MethodPut, appPath(app, "bucket"), map[string]string{"bucket": bucket}, &answer)
	return answer, err
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
// bucket is the bucket of the snapshot when it is an older one the app
// left; empty is the app's bucket.
func (c *Client) Restore(ctx context.Context, app, database, snapshot, bucket string, at *time.Time) error {
	body := map[string]any{"database": database, "snapshot": snapshot}
	if bucket != "" {
		body["bucket"] = bucket
	}
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
