package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/protocol"
)

const (
	backupTimeout  = 10 * time.Minute
	restoreTimeout = 15 * time.Minute
)

var (
	ErrBackupEndpoint = errors.New("the endpoint must be an https:// URL, or empty for AWS S3")
	ErrBackupBucket   = errors.New("bucket, access key and secret key are required")
	ErrBackupAccess   = errors.New("moin could not write to the bucket with these keys")
	ErrBucketPrivate  = errors.New("the endpoint points to a private or local address: moin only reaches buckets on the public internet")
	ErrNoBackups      = errors.New("backups are not set up: add a bucket first")
	ErrRestoreTarget  = errors.New("choose a snapshot or a point in time")
	ErrRestoreOlder   = errors.New("a point in time restore reads from the app's bucket: pick a snapshot to restore from an older bucket")
	ErrSnapshotID     = errors.New("this is not a snapshot ID: copy one from the backups list")
)

// AllowOnlyPublicBuckets makes the bucket check refuse private and local
// addresses. The hosted control plane turns it on: otherwise a user could
// make it call into its own network. A self-hosted one may use a bucket
// next to it.
func (s *AppService) AllowOnlyPublicBuckets() { s.publicBucketsOnly = true }

// checkBucket writes and deletes a small object in the workspace's folder,
// which needs the same rights as backups. publicOnly refuses addresses that
// are not on the public internet.
func checkBucket(ctx context.Context, storage protocol.BackupStorage, publicOnly bool) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := bucketClient(storage)
	if publicOnly {
		client = s3.New(client.Options(), func(options *s3.Options) {
			options.HTTPClient = publicHTTPClient()
			// A refused address stays refused: do not try it again.
			options.Retryer = retry.NewStandard(func(retries *retry.StandardOptions) {
				refused := retry.IsErrorRetryableFunc(func(err error) aws.Ternary {
					if errors.Is(err, ErrBucketPrivate) {
						return aws.FalseTernary
					}
					return aws.UnknownTernary
				})
				retries.Retryables = append([]retry.IsErrorRetryable{refused}, retries.Retryables...)
			})
		})
	}
	key := strings.TrimLeft(path.Join(storage.Path, storage.Workspace, ".ops-check"), "/")
	_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(storage.Bucket), Key: aws.String(key), Body: bytes.NewReader([]byte("moin"))})
	if err != nil {
		return err
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(storage.Bucket), Key: aws.String(key)})
	return err
}

// publicHTTPClient connects only to public addresses. The check runs when
// the connection opens, after DNS: a name that points inside cannot slip
// past it. It goes without a proxy, so the check sees the bucket itself.
func publicHTTPClient() *awshttp.BuildableClient {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: refusePrivate}
	return awshttp.NewBuildableClient().WithTransportOptions(func(transport *http.Transport) {
		transport.Proxy = nil
		transport.DialContext = dialer.DialContext
	})
}

// sharedNetwork is 100.64.0.0/10: carrier networks and Tailscale.
var sharedNetwork = netip.MustParsePrefix("100.64.0.0/10")

// refusePrivate stops a connection to loopback, private, link-local,
// shared or unspecified addresses, like 127.0.0.1, 10.0.0.5, 169.254.169.254
// or 0.0.0.0.
func refusePrivate(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || sharedNetwork.Contains(ip) {
		return ErrBucketPrivate
	}
	return nil
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

// backupConfig is what the agent of a server gets: the default bucket in
// plain text, and the bucket of each of its apps that uses another one,
// or nothing.
func (s *AppService) backupConfig(workspaceID, serverID string) protocol.BackupConfig {
	bucket, err := s.apps.DefaultBucket(workspaceID)
	if err != nil {
		return protocol.BackupConfig{}
	}
	storage, err := s.openBucket(bucket)
	if err != nil {
		return protocol.BackupConfig{}
	}
	config := protocol.BackupConfig{Storage: storage}
	apps, err := s.apps.ListByWorkspaceID(workspaceID)
	if err != nil {
		slog.Error("list apps for backups failed", "workspace_id", workspaceID, "error", err)
		return config
	}
	for _, app := range apps {
		if app.ServerID != serverID || app.BucketID == nil || *app.BucketID == bucket.ID {
			continue
		}
		own, err := s.appStorage(&app)
		if err != nil {
			continue
		}
		if config.AppStorage == nil {
			config.AppStorage = map[string]*protocol.BackupStorage{}
		}
		config.AppStorage[app.Name] = own
	}
	return config
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
	config := s.backupConfig(workspaceID, serverID)
	config.Apps = s.appIDs(serverID, workspaceID)
	err := s.servers.Request(ctx, serverID, protocol.TypeBackupConfig, config, nil)
	if err != nil {
		slog.Warn("send backup config failed", "server_id", serverID, "error", err)
	}
}

// appIDs are the IDs of a server's apps by name.
func (s *AppService) appIDs(serverID, workspaceID string) map[string]string {
	apps, err := s.apps.ListByWorkspaceID(workspaceID)
	if err != nil {
		slog.Error("list apps for backups failed", "workspace_id", workspaceID, "error", err)
		return nil
	}
	ids := map[string]string{}
	for _, app := range apps {
		if app.ServerID == serverID {
			ids[app.Name] = app.ID
		}
	}
	return ids
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
	_, err = s.AppBucket(app)
	if err != nil {
		return app, protocol.Backups{}, err
	}
	request := protocol.BackupRequest{App: app.Name, Reason: reason, Older: s.olderStorage(app)}
	ctx, cancel := context.WithTimeout(ctx, backupTimeout)
	defer cancel()
	var backups protocol.Backups
	err = s.servers.Request(ctx, app.ServerID, messageType, request, &backups)
	if err != nil {
		return app, backups, err
	}
	s.storeBackupStatus(app, backups)
	return app, backups, nil
}

// Restore puts a database of an app back from a snapshot or to a point in
// time. The agent takes a snapshot of the current state first. bucket names
// the bucket the snapshot is in, when it is one the app left; empty is the
// app's bucket. A point in time comes from the app's bucket.
func (s *AppService) Restore(ctx context.Context, actor Actor, idOrName, database, snapshot, bucket string, at *time.Time) error {
	if snapshot == "" && at == nil {
		return ErrRestoreTarget
	}
	// Only IDs the agent makes go on, as part of a bucket key.
	if snapshot != "" && !protocol.ValidSnapshotID(snapshot) {
		return ErrSnapshotID
	}
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return err
	}
	current, err := s.AppBucket(app)
	if err != nil {
		return err
	}
	request := protocol.BackupRestore{App: app.Name, Database: database, Snapshot: snapshot, At: at}
	if bucket != "" && bucket != current.Name {
		if snapshot == "" {
			return ErrRestoreOlder
		}
		older, err := s.olderBucket(app, bucket)
		if err != nil {
			return err
		}
		request.Storage, err = s.openBucket(older)
		if err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, restoreTimeout)
	defer cancel()
	err = s.servers.Request(ctx, app.ServerID, protocol.TypeBackupRestore, request, nil)
	if err != nil {
		return err
	}
	slog.Info("database restored", "app_id", app.ID, "database", database, "snapshot", snapshot, "at", at)
	details := map[string]string{"database": database, "snapshot": snapshot}
	if request.Storage != nil {
		details["bucket"] = bucket
	}
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
	if app.SnapshotBeforeChanges == on {
		return app, nil
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

// snapshotBeforeWrite says whether a write through moin takes a snapshot
// first.
func (s *AppService) snapshotBeforeWrite(app *model.App) bool {
	if !app.SnapshotBeforeChanges {
		return false
	}
	_, err := s.AppBucket(app)
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

// storeBackupStatus stores the backup status of each database. Backups
// that start failing or work again, a restore check that fails or passes
// again, and each passed check go into the activity as moin itself; the
// changes alert. The backups' own error and the check's are apart: an
// agent from before drill_error sends the check's failure as Error alone.
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
		status := model.BackupStatus{
			AppID:        appID,
			Database:     database.Database,
			Since:        database.Since,
			ReplicatedAt: database.ReplicatedAt,
			VerifiedAt:   database.VerifiedAt,
			Error:        clipText(database.Error, 2000),
			DrillError:   clipText(database.DrillError, 2000),
			UpdatedAt:    s.now().UTC(),
		}
		if database.VerifiedAt == nil {
			// No check since the agent started: the last one still stands.
			status.DrillError = previous.DrillError
		}
		for _, change := range backupChanges(previous, known, status) {
			s.servers.record(System(app.WorkspaceID), change.action, "app", app.Name, change.details)
		}
		err := s.apps.SetBackupStatus(status)
		if err != nil {
			slog.Error("store backup status failed", "app_id", appID, "error", fmt.Sprint(err))
		}
	}
}

// backupChange is one action of the activity a backup status brings.
type backupChange struct {
	action  string
	details map[string]string
}

// backupChanges compares a database's backup status with the one before
// and says what changed: backups that fail or work again, a restore check
// that fails or passes again, and a passed check. Only a change counts, so
// an agent that tells the same failure every minute alerts once.
func backupChanges(previous model.BackupStatus, known bool, now model.BackupStatus) []backupChange {
	changes := []backupChange{}
	add := func(action string, details map[string]string) {
		details["database"] = now.Database
		changes = append(changes, backupChange{action: action, details: details})
	}
	backupError, before := now.BackupError(), previous.BackupError()
	switch {
	case backupError != "" && (!known || before == ""):
		add("backup.failed", map[string]string{"error": clipText(backupError, 300)})
	case backupError == "" && known && before != "":
		add("backup.recovered", map[string]string{})
	}
	if now.VerifiedAt == nil {
		return changes
	}
	switch {
	case now.DrillError != "" && (!known || previous.DrillError == ""):
		add("drill.failed", map[string]string{"error": clipText(now.DrillError, 300)})
	case now.DrillError == "" && known && previous.DrillError != "":
		add("drill.recovered", map[string]string{})
	}
	if now.DrillError == "" && (!known || previous.VerifiedAt == nil || now.VerifiedAt.After(*previous.VerifiedAt)) {
		add("backup.drill_passed", map[string]string{"verified_at": now.VerifiedAt.UTC().Format(time.RFC3339)})
	}
	return changes
}
