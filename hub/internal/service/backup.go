package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/protocol"
)

const (
	backupTimeout = 10 * time.Minute
	// restoreTimeout is how long the hub waits for a restore.
	// The daemon runs it to the end in any case.
	restoreTimeout = 3 * time.Hour
)

var (
	ErrBackupEndpoint  = errors.New("the endpoint must be an https:// URL, or empty for AWS S3")
	ErrBackupBucket    = errors.New("bucket, access key and secret key are required")
	ErrNoBackups       = errors.New("backups are not set up: add a bucket first")
	ErrRestoreTarget   = errors.New("pick a moment to go back to")
	ErrMoment          = errors.New("there is no such moment: pick one from the list")
	ErrMomentOutside   = errors.New("there is no copy that old: pick a moment from the list")
	ErrRestoreDatabase = errors.New("the app has several databases: name the one")
	ErrRestoreRunning  = errors.New("a restore of this app runs: wait until it is done")
	ErrRestoreNotFound = errors.New("there is no such restore: it is older than a week, or of another app")
	ErrNoCopy          = errors.New("there is no copy yet: the first one is taken within a minute of a deploy with a bucket")
	ErrBackupEvery     = errors.New("how often: every 15 minutes, hourly or daily")
	ErrBackupKeep      = errors.New("keep: 1, 3, 7 or 30 days")
)

// AllowOnlyPublicAddresses makes the uptime checks refuse private and
// local addresses. The hosted hub turns it on: otherwise a user could make
// it call into its own network. A self-hosted one may check an app next
// to it.
func (s *AppService) AllowOnlyPublicAddresses() { s.publicOnly = true }

// backupConfig is what the daemon of a server gets: the default bucket,
// the bucket of each of its apps that uses another one, and the schedule
// of each; nothing without a bucket. The server fills in the secret keys
// it keeps; an app whose bucket key the server does not keep is paused,
// the others go on.
func (s *AppService) backupConfig(workspaceID, serverID string) (protocol.BackupConfig, error) {
	bucket, err := s.apps.DefaultBucket(workspaceID)
	if errors.Is(err, repository.ErrNoBucket) {
		return protocol.BackupConfig{}, nil
	}
	if err != nil {
		return protocol.BackupConfig{}, err
	}
	index, err := s.servers.secretsIndex(workspaceID)
	if err != nil {
		return protocol.BackupConfig{}, err
	}
	config := protocol.BackupConfig{Bucket: daemonBucket(bucket), Schedules: map[string]protocol.Schedule{}}
	apps, err := s.apps.ListByWorkspaceID(workspaceID)
	if err != nil {
		return protocol.BackupConfig{}, err
	}
	for _, app := range apps {
		if app.ServerID != serverID {
			continue
		}
		config.Schedules[app.Folder] = scheduleOf(&app)
		own, err := s.appBucket(&app)
		if err != nil {
			return protocol.BackupConfig{}, err
		}
		if !index.has(serverID, protocol.BucketSecret(own.ID)) {
			if config.Paused == nil {
				config.Paused = map[string]string{}
			}
			config.Paused[app.Folder] = missingBucketKey(index, serverID, own.Name).Error()
		}
		if own.ID == bucket.ID {
			continue
		}
		if config.AppBuckets == nil {
			config.AppBuckets = map[string]*protocol.Bucket{}
		}
		config.AppBuckets[app.Folder] = daemonBucket(own)
	}
	return config, nil
}

// missingBucketKey says that a server keeps no key for a bucket, and where
// to enter it.
func missingBucketKey(index secretsIndex, serverID, bucket string) error {
	name := serverID
	for _, server := range index.servers {
		if server.ID == serverID {
			name = server.Name
		}
	}
	return &MissingSecretError{Message: name + " has no key for the bucket " + bucket + ". Enter it for " + name + " under Backups."}
}

// reconfigure sends the backup config to a server, when it is online, on
// its own: an app came or went, so its schedule does too.
func (s *AppService) reconfigure(serverID, workspaceID string) {
	if s.servers.Online(serverID) {
		go s.configureDaemon(serverID, workspaceID)
	}
}

// configureDaemons sends the backup config to every online server of the
// user and waits for them, so the next request finds it in place. One
// that is offline gets it when it connects.
func (s *AppService) configureDaemons(workspaceID string) {
	servers, err := s.servers.servers.ListByWorkspaceID(workspaceID)
	if err != nil {
		slog.Error("list servers for backups failed", "workspace_id", workspaceID, "error", err)
		return
	}
	var wait sync.WaitGroup
	for _, server := range servers {
		if s.servers.Online(server.ID) {
			wait.Go(func() { s.configureDaemon(server.ID, workspaceID) })
		}
	}
	wait.Wait()
}

// configuring keeps the sends to one server in a row, each with the
// config as it is then: the last one sent is the latest.
var configuring sync.Map

func (s *AppService) configureDaemon(serverID, workspaceID string) {
	lock, _ := configuring.LoadOrStore(serverID, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config, err := s.backupConfig(workspaceID, serverID)
	if err != nil {
		slog.Error("the backup config is not sent", "server_id", serverID, "error", err)
		return
	}
	config.Apps = s.appIDs(serverID, workspaceID)
	err = s.servers.Request(ctx, serverID, protocol.TypeBackupConfig, config, nil)
	if err != nil {
		slog.Warn("send backup config failed", "server_id", serverID, "error", err)
	}
}

// appIDs are the IDs of a server's apps by folder, the name the
// daemon knows each by.
func (s *AppService) appIDs(serverID, workspaceID string) map[string]string {
	apps, err := s.apps.ListByWorkspaceID(workspaceID)
	if err != nil {
		slog.Error("list apps for backups failed", "workspace_id", workspaceID, "error", err)
		return nil
	}
	ids := map[string]string{}
	for _, app := range apps {
		if app.ServerID == serverID {
			ids[app.Folder] = app.ID
		}
	}
	return ids
}
