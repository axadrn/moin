package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/protocol"
)

const (
	backupTimeout  = 10 * time.Minute
	restoreTimeout = 15 * time.Minute
)

var (
	ErrBackupEndpoint = errors.New("the endpoint must be an https:// URL, or empty for AWS S3")
	ErrBackupBucket   = errors.New("bucket, access key and secret key are required")
	ErrBackupAccess   = errors.New("ops could not write to the bucket with these keys")
	ErrNoBackups      = errors.New("backups are not set up: add a bucket first")
	ErrRestoreTarget  = errors.New("choose a snapshot or a point in time")
)

// BackupStorage is an account's bucket without its secret key. Folder is
// where the workspace's files go in it: <path>/<workspace ID>/.
type BackupStorage struct {
	Endpoint  string
	Region    string
	Bucket    string
	Path      string
	Folder    string
	AccessKey string
	CreatedAt time.Time
}

// AppFolder is where an app's uploads and backups go in the bucket:
// <path>/<workspace ID>/<app>/<app ID>/.
func (s BackupStorage) AppFolder(app *model.App) string {
	return s.Folder + app.Name + "/" + app.ID + "/"
}

// BackupStorageInput is a bucket as people enter it.
type BackupStorageInput struct {
	Endpoint  string
	Region    string
	Bucket    string
	Path      string
	AccessKey string
	SecretKey string
}

// BackupStorage returns the account's bucket, or ErrNoBackups.
func (s *AppService) BackupStorage(workspaceID string) (*BackupStorage, error) {
	storage, err := s.apps.BackupStorage(workspaceID)
	if errors.Is(err, repository.ErrNoBackupStorage) {
		return nil, ErrNoBackups
	}
	if err != nil {
		return nil, err
	}
	folder := strings.TrimLeft(path.Join(storage.Path, workspaceID), "/") + "/"
	return &BackupStorage{Endpoint: storage.Endpoint, Region: storage.Region, Bucket: storage.Bucket, Path: storage.Path, Folder: folder, AccessKey: storage.AccessKey, CreatedAt: storage.CreatedAt}, nil
}

// SetBackupStorage checks that ops can write to the bucket, stores it and
// tells the user's online agents to back up there.
func (s *AppService) SetBackupStorage(ctx context.Context, actor Actor, input BackupStorageInput) error {
	storage := protocol.BackupStorage{
		Endpoint:  strings.TrimRight(strings.TrimSpace(input.Endpoint), "/"),
		Region:    strings.TrimSpace(input.Region),
		Bucket:    strings.TrimSpace(input.Bucket),
		Path:      strings.Trim(strings.TrimSpace(input.Path), "/"),
		Workspace: actor.WorkspaceID,
		AccessKey: strings.TrimSpace(input.AccessKey),
		SecretKey: strings.TrimSpace(input.SecretKey),
	}
	if storage.Endpoint != "" {
		parsed, err := url.Parse(storage.Endpoint)
		local := parsed != nil && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1")
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && local)) {
			return ErrBackupEndpoint
		}
	}
	if storage.Bucket == "" || storage.AccessKey == "" || storage.SecretKey == "" {
		return ErrBackupBucket
	}
	if storage.Region == "" {
		storage.Region = "auto"
	}
	err := checkBucket(ctx, storage)
	if err != nil {
		slog.Info("backup bucket check failed", "workspace_id", actor.WorkspaceID, "error", err)
		return ErrBackupAccess
	}
	err = s.apps.SetBackupStorage(model.BackupStorage{
		WorkspaceID: actor.WorkspaceID,
		Endpoint:    storage.Endpoint,
		Region:      storage.Region,
		Bucket:      storage.Bucket,
		Path:        storage.Path,
		AccessKey:   storage.AccessKey,
		SecretKey:   s.box.Seal(storage.SecretKey, actor.WorkspaceID+"/backups"),
		CreatedAt:   s.now().UTC(),
	})
	if err != nil {
		return err
	}
	s.configureAgents(actor.WorkspaceID)
	s.servers.record(actor, "bucket.set", "bucket", storage.Bucket, map[string]string{"endpoint": storage.Endpoint, "path": storage.Path})
	s.notify(actor.WorkspaceID)
	return nil
}

// RemoveBackupStorage forgets the bucket; agents stop backing up. What is
// in the bucket stays.
func (s *AppService) RemoveBackupStorage(actor Actor) error {
	err := s.apps.DeleteBackupStorage(actor.WorkspaceID)
	if errors.Is(err, repository.ErrNoBackupStorage) {
		return ErrNoBackups
	}
	if err != nil {
		return err
	}
	s.configureAgents(actor.WorkspaceID)
	s.servers.record(actor, "bucket.removed", "bucket", "", nil)
	s.notify(actor.WorkspaceID)
	return nil
}

// checkBucket writes and deletes a small object in the workspace's folder,
// which needs the same rights as backups.
func checkBucket(ctx context.Context, storage protocol.BackupStorage) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := bucketClient(storage)
	key := strings.TrimLeft(path.Join(storage.Path, storage.Workspace, ".ops-check"), "/")
	_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(storage.Bucket), Key: aws.String(key), Body: bytes.NewReader([]byte("ops"))})
	if err != nil {
		return err
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(storage.Bucket), Key: aws.String(key)})
	return err
}

func bucketClient(storage protocol.BackupStorage) *s3.Client {
	endpoint := storage.Endpoint
	if endpoint == "" {
		region := storage.Region
		if region == "" || region == "auto" {
			region = "us-east-1"
		}
		endpoint = "https://s3." + region + ".amazonaws.com"
	}
	return s3.New(s3.Options{
		Region:       storage.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(storage.AccessKey, storage.SecretKey, ""),
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: storage.Endpoint != "",
	})
}

// backupConfig is what the agents of a user get: the bucket in plain
// text, or nothing.
func (s *AppService) backupConfig(workspaceID string) protocol.BackupConfig {
	storage, err := s.apps.BackupStorage(workspaceID)
	if err != nil {
		return protocol.BackupConfig{}
	}
	secretKey, err := s.box.Open(storage.SecretKey, workspaceID+"/backups")
	if err != nil {
		slog.Error("open backup secret failed", "workspace_id", workspaceID, "error", err)
		return protocol.BackupConfig{}
	}
	return protocol.BackupConfig{Storage: &protocol.BackupStorage{
		Endpoint:  storage.Endpoint,
		Region:    storage.Region,
		Bucket:    storage.Bucket,
		Path:      storage.Path,
		Workspace: workspaceID,
		AccessKey: storage.AccessKey,
		SecretKey: secretKey,
	}}
}

// configureAgents sends the backup config to every online server of the
// user and waits for them, so the next request finds it in place. One
// that is offline gets it when it connects.
func (s *AppService) configureAgents(workspaceID string) {
	servers, err := s.servers.servers.ListByWorkspaceID(workspaceID)
	if err != nil {
		slog.Error("list servers for backups failed", "workspace_id", workspaceID, "error", err)
		return
	}
	var wait sync.WaitGroup
	for _, server := range servers {
		if s.servers.Online(server.ID) {
			wait.Go(func() { s.configureAgent(server.ID, workspaceID) })
		}
	}
	wait.Wait()
}

func (s *AppService) configureAgent(serverID, workspaceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := s.servers.Request(ctx, serverID, protocol.TypeBackupConfig, s.backupConfig(workspaceID), nil)
	if err != nil {
		slog.Warn("send backup config failed", "server_id", serverID, "error", err)
	}
}

// Backups asks the agent for the snapshots and the restore window of an
// app's databases.
func (s *AppService) Backups(ctx context.Context, workspaceID, idOrName string) (*model.App, protocol.Backups, error) {
	return s.backupRequest(ctx, workspaceID, idOrName, protocol.TypeBackupList, "")
}

// BackUp takes a snapshot of an app's databases now.
func (s *AppService) BackUp(ctx context.Context, actor Actor, idOrName string) (*model.App, protocol.Backups, error) {
	app, backups, err := s.backupRequest(ctx, actor.WorkspaceID, idOrName, protocol.TypeBackupSnapshot, "manual")
	if err == nil {
		s.servers.record(actor, "backup.taken", "app", app.Name, nil)
	}
	return app, backups, err
}

func (s *AppService) backupRequest(ctx context.Context, workspaceID, idOrName string, messageType protocol.MessageType, reason string) (*model.App, protocol.Backups, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, protocol.Backups{}, err
	}
	_, err = s.BackupStorage(workspaceID)
	if err != nil {
		return app, protocol.Backups{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, backupTimeout)
	defer cancel()
	var backups protocol.Backups
	err = s.servers.Request(ctx, app.ServerID, messageType, protocol.BackupRequest{App: app.Name, Reason: reason}, &backups)
	if err != nil {
		return app, backups, err
	}
	s.storeBackupStatus(app, backups)
	return app, backups, nil
}

// Restore puts a database of an app back from a snapshot or to a point in
// time. The agent takes a snapshot of the current state first.
func (s *AppService) Restore(ctx context.Context, actor Actor, idOrName, database, snapshot string, at *time.Time) error {
	if snapshot == "" && at == nil {
		return ErrRestoreTarget
	}
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return err
	}
	_, err = s.BackupStorage(actor.WorkspaceID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, restoreTimeout)
	defer cancel()
	err = s.servers.Request(ctx, app.ServerID, protocol.TypeBackupRestore, protocol.BackupRestore{App: app.Name, Database: database, Snapshot: snapshot, At: at}, nil)
	if err != nil {
		return err
	}
	slog.Info("database restored", "app_id", app.ID, "database", database, "snapshot", snapshot, "at", at)
	details := map[string]string{"database": database, "snapshot": snapshot}
	if at != nil {
		details["at"] = at.UTC().Format(time.RFC3339)
	}
	s.servers.record(actor, "backup.restored", "app", app.Name, details)
	s.notify(actor.WorkspaceID)
	return nil
}

// SetSnapshotBeforeChanges turns the snapshot before every change on or
// off for an app.
func (s *AppService) SetSnapshotBeforeChanges(actor Actor, idOrName string, on bool) (*model.App, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	err = s.apps.SetSnapshotBeforeChanges(app.ID, on)
	if err != nil {
		return nil, err
	}
	s.servers.record(actor, "backup.snapshot_before_changes", "app", app.Name, map[string]string{"on": strconv.FormatBool(on)})
	s.notify(actor.WorkspaceID)
	return s.apps.ByID(app.ID)
}

// BackupStatus is what the agent last reported about the app's databases.
func (s *AppService) BackupStatus(workspaceID, idOrName string) ([]model.BackupStatus, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.apps.BackupStatus(app.ID)
}

// snapshotBeforeWrite says whether a write through ops takes a snapshot
// first.
func (s *AppService) snapshotBeforeWrite(app *model.App) bool {
	if !app.SnapshotBeforeChanges {
		return false
	}
	_, err := s.apps.BackupStorage(app.WorkspaceID)
	return err == nil
}

// receiveBackupStatus stores a status the agent sent on its own.
func (s *AppService) receiveBackupStatus(serverID string, backups protocol.Backups) {
	workspaceID, ok := s.servers.UserOf(serverID)
	if !ok {
		return
	}
	app, err := s.apps.ByIDOrNameForWorkspace(backups.App, workspaceID)
	if err != nil || app.ServerID != serverID {
		return
	}
	s.storeBackupStatus(app, backups)
	s.notify(workspaceID)
}

// storeBackupStatus stores the backup status of each database. A restore
// drill that passed, and backups that start failing or work again, go into
// the activity as ops itself.
func (s *AppService) storeBackupStatus(app *model.App, backups protocol.Backups) {
	appID := app.ID
	before := map[string]model.BackupStatus{}
	if statuses, err := s.apps.BackupStatus(appID); err == nil {
		for _, status := range statuses {
			before[status.Database] = status
		}
	}
	for _, database := range backups.Databases {
		previous, known := before[database.Database]
		details := map[string]string{"database": database.Database}
		switch {
		case database.Error != "" && (!known || previous.Error == ""):
			details["error"] = clipText(database.Error, 300)
			s.servers.record(System(app.WorkspaceID), "backup.failed", "app", app.Name, details)
		case database.Error == "" && known && previous.Error != "":
			s.servers.record(System(app.WorkspaceID), "backup.recovered", "app", app.Name, details)
		}
		if database.VerifiedAt != nil && (!known || previous.VerifiedAt == nil || database.VerifiedAt.After(*previous.VerifiedAt)) {
			details := map[string]string{"database": database.Database, "verified_at": database.VerifiedAt.UTC().Format(time.RFC3339)}
			s.servers.record(System(app.WorkspaceID), "backup.drill_passed", "app", app.Name, details)
		}
		err := s.apps.SetBackupStatus(model.BackupStatus{
			AppID:        appID,
			Database:     database.Database,
			Since:        database.Since,
			ReplicatedAt: database.ReplicatedAt,
			VerifiedAt:   database.VerifiedAt,
			Error:        clipText(database.Error, 2000),
			UpdatedAt:    s.now().UTC(),
		})
		if err != nil {
			slog.Error("store backup status failed", "app_id", appID, "error", fmt.Sprint(err))
		}
	}
}
