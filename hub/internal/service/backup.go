package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/internal/build"
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
	ErrBackupAccess    = errors.New("moin could not write to the bucket with these keys")
	ErrBucketPrivate   = errors.New("the endpoint points to a private or local address: moin only reaches buckets on the public internet")
	ErrNoBackups       = errors.New("backups are not set up: add a bucket first")
	ErrRestoreTarget   = errors.New("pick a moment to go back to")
	ErrMoment          = errors.New("there is no such moment: pick one from the list")
	ErrMomentOutside   = errors.New("moin cannot go back that far: pick a moment from the list")
	ErrRestoreDatabase = errors.New("the app has several databases: name the one")
	ErrRestoreRunning  = errors.New("a restore of this app runs: wait until it is done")
	ErrRestoreNotFound = errors.New("there is no such restore: it is older than a week, or of another app")
	ErrNoCopy          = errors.New("there is no copy yet: the first one is taken within a minute of a deploy with a bucket")
	ErrDownloadNewest  = errors.New("a SQLite database downloads its newest state: leave out the moment")
	ErrScheduleInFile  = errors.New("the backup schedule is set in compose.yml: change it there and deploy")
	ErrBackupEvery     = errors.New("how often: every 15 minutes, hourly or daily")
	ErrBackupKeep      = errors.New("keep: 1, 3, 7 or 30 days")
)

// AllowOnlyPublicBuckets makes the bucket check refuse private and local
// addresses. The hosted hub turns it on: otherwise a user could
// make it call into its own network. A self-hosted one may use a bucket
// next to it.
func (s *AppService) AllowOnlyPublicBuckets() { s.publicBucketsOnly = true }

// checkBucket writes and deletes a small object in the workspace's folder,
// which needs the same rights as backups. publicOnly refuses addresses that
// are not on the public internet.
func checkBucket(ctx context.Context, bucket protocol.Bucket, publicOnly bool) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client := bucketClient(bucket)
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
	key := strings.TrimLeft(path.Join(bucket.Path, bucket.Workspace, "."+build.Name+"-check"), "/")
	_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket.Bucket), Key: aws.String(key), Body: bytes.NewReader([]byte("moin"))})
	if err != nil {
		return err
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket.Bucket), Key: aws.String(key)})
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

func bucketClient(bucket protocol.Bucket) *s3.Client {
	endpoint := bucket.Endpoint
	if endpoint == "" {
		region := bucket.Region
		if region == "" || region == "auto" {
			region = "us-east-1"
		}
		endpoint = "https://s3." + region + ".amazonaws.com"
	}
	return s3.New(s3.Options{
		Region:       bucket.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(bucket.AccessKey, bucket.SecretKey, ""),
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: bucket.Endpoint != "",
	})
}

// backupConfig is what the daemon of a server gets: the default bucket in
// plain text, the bucket of each of its apps that uses another one, and
// the schedule of each; nothing without a bucket. An app whose bucket
// cannot be opened is paused, the others go on. An error means none of
// it can be told: half a config would stop or prune backups.
func (s *AppService) backupConfig(workspaceID, serverID string) (protocol.BackupConfig, error) {
	bucket, err := s.apps.DefaultBucket(workspaceID)
	if errors.Is(err, repository.ErrNoBucket) {
		return protocol.BackupConfig{}, nil
	}
	if err != nil {
		return protocol.BackupConfig{}, err
	}
	open, err := s.openBucket(bucket)
	if err != nil {
		return protocol.BackupConfig{}, err
	}
	config := protocol.BackupConfig{Bucket: open, Schedules: map[string]protocol.Schedule{}}
	apps, err := s.apps.ListByWorkspaceID(workspaceID)
	if err != nil {
		return protocol.BackupConfig{}, err
	}
	for _, app := range apps {
		if app.ServerID == serverID {
			config.Schedules[app.Folder] = scheduleOf(&app)
		}
		if app.ServerID != serverID || app.BucketID == nil || *app.BucketID == bucket.ID {
			continue
		}
		own, err := s.openAppBucket(&app)
		if err != nil {
			if config.Paused == nil {
				config.Paused = map[string]string{}
			}
			config.Paused[app.Folder] = "the bucket of " + app.Name + " cannot be opened: " + err.Error()
			continue
		}
		if config.AppBuckets == nil {
			config.AppBuckets = map[string]*protocol.Bucket{}
		}
		config.AppBuckets[app.Folder] = own
	}
	return config, nil
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
