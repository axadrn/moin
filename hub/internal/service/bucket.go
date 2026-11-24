package service

import (
	"context"
	"errors"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/protocol"
	"github.com/google/uuid"
)

// A workspace keeps a list of named buckets. One is its default: apps
// without a bucket of their own use it. Each app may pick another one; its
// new backups go there. The bucket it left keeps its older
// backups, and restores of those read from there.

// ErrBucketName refuses a bucket name that is not a short slug.
var ErrBucketName = errors.New("a bucket's name is 1 to 63 lowercase letters, digits or dashes")

// BucketInUseError refuses to remove a bucket that apps use or that keeps
// older backups of an app.
type BucketInUseError struct {
	Bucket string
	// Apps back up there now; Older left it and keep their
	// older backups there.
	Apps, Older []string
}

func (e *BucketInUseError) Error() string {
	var reasons []string
	if len(e.Apps) > 0 {
		reasons = append(reasons, "the backups of "+strings.Join(e.Apps, ", ")+" go there; move them to another bucket first")
	}
	if len(e.Older) > 0 {
		reasons = append(reasons, "it keeps what "+strings.Join(e.Older, ", ")+" left there")
	}
	return "The bucket " + e.Bucket + " is in use: " + strings.Join(reasons, ", and ") + "."
}

// Bucket is one bucket of a workspace without its secret key, which only
// the servers keep: Servers says which. Folder is where the workspace's
// files go in it: <path>/<workspace ID>/. Apps use it now; Older are the
// apps that left it and keep older backups there.
type Bucket struct {
	ID        string
	Name      string
	Default   bool
	Endpoint  string
	Region    string
	Bucket    string
	Path      string
	Folder    string
	AccessKey string
	CreatedAt time.Time
	Apps      []string
	Older     []string
	Servers   SecretServers
}

// AppFolder is where an app's backups go in the bucket:
// <path>/<workspace ID>/<app>/<app ID>/.
func (b Bucket) AppFolder(app *model.App) string {
	return b.Folder + app.Folder + "/" + app.ID + "/"
}

// BucketInput is a bucket as people enter it. Server is the server that
// checks it and keeps SecretKey, empty for the workspace's only one. An
// empty SecretKey keeps the server's key when a bucket changes. Default
// makes it the workspace's default bucket.
type BucketInput struct {
	Server    string
	Name      string
	Endpoint  string
	Region    string
	Bucket    string
	Path      string
	AccessKey string
	SecretKey string
	Default   bool
}

func newBucket(bucket model.Bucket) Bucket {
	return Bucket{
		ID:        bucket.ID,
		Name:      bucket.Name,
		Default:   bucket.IsDefault,
		Endpoint:  bucket.Endpoint,
		Region:    bucket.Region,
		Bucket:    bucket.Bucket,
		Path:      bucket.Path,
		Folder:    strings.TrimLeft(path.Join(bucket.Path, bucket.WorkspaceID), "/") + "/",
		AccessKey: bucket.AccessKey,
		CreatedAt: bucket.CreatedAt,
		Apps:      []string{},
		Older:     []string{},
	}
}

// withApps adds the apps that use the bucket and the servers that keep
// its key.
func (s *AppService) withApps(bucket model.Bucket, index secretsIndex) (Bucket, error) {
	view := newBucket(bucket)
	var err error
	view.Apps, view.Older, err = s.apps.BucketApps(bucket.WorkspaceID, bucket.ID)
	view.Servers = index.of(protocol.BucketSecret(bucket.ID))
	return view, err
}

// Buckets lists the buckets of a workspace, the default first, each with
// the apps that use it.
func (s *AppService) Buckets(workspaceID string) ([]Bucket, error) {
	buckets, err := s.apps.Buckets(workspaceID)
	if err != nil {
		return nil, err
	}
	index, err := s.servers.secretsIndex(workspaceID)
	if err != nil {
		return nil, err
	}
	views := make([]Bucket, 0, len(buckets))
	for _, bucket := range buckets {
		view, err := s.withApps(bucket, index)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

// Bucket returns one bucket of a workspace by name.
func (s *AppService) Bucket(workspaceID, name string) (*Bucket, error) {
	bucket, err := s.apps.Bucket(workspaceID, name)
	if err != nil {
		return nil, err
	}
	index, err := s.servers.secretsIndex(workspaceID)
	if err != nil {
		return nil, err
	}
	view, err := s.withApps(*bucket, index)
	return &view, err
}

// DefaultBucket returns the workspace's default bucket, or ErrNoBackups.
func (s *AppService) DefaultBucket(workspaceID string) (*Bucket, error) {
	bucket, err := s.apps.DefaultBucket(workspaceID)
	if errors.Is(err, repository.ErrNoBucket) {
		return nil, ErrNoBackups
	}
	if err != nil {
		return nil, err
	}
	index, err := s.servers.secretsIndex(workspaceID)
	if err != nil {
		return nil, err
	}
	view, err := s.withApps(*bucket, index)
	return &view, err
}

// AppBucket returns the bucket of an app: its own, or the workspace's
// default. ErrNoBackups means there is none.
func (s *AppService) AppBucket(app *model.App) (*Bucket, error) {
	bucket, err := s.appBucket(app)
	if err != nil {
		return nil, err
	}
	view := newBucket(*bucket)
	return &view, nil
}

func (s *AppService) appBucket(app *model.App) (*model.Bucket, error) {
	if app.BucketID != nil {
		buckets, err := s.apps.Buckets(app.WorkspaceID)
		if err != nil {
			return nil, err
		}
		for _, bucket := range buckets {
			if bucket.ID == *app.BucketID {
				return &bucket, nil
			}
		}
	}
	bucket, err := s.apps.DefaultBucket(app.WorkspaceID)
	if errors.Is(err, repository.ErrNoBucket) {
		return nil, ErrNoBackups
	}
	return bucket, err
}

// OlderBuckets are the buckets an app left, newest first. Its older
// backups stay there.
func (s *AppService) OlderBuckets(app *model.App) ([]Bucket, error) {
	buckets, err := s.apps.OlderBuckets(app.ID)
	if err != nil {
		return nil, err
	}
	views := make([]Bucket, 0, len(buckets))
	for _, bucket := range buckets {
		views = append(views, newBucket(bucket))
	}
	return views, nil
}

// AddBucket has a server check that moin can write to a new bucket and
// keep its secret key, then stores the bucket without the key. The first
// bucket of a workspace is its default.
func (s *AppService) AddBucket(ctx context.Context, actor Actor, input BucketInput) (*Bucket, error) {
	clean, err := cleanBucket(actor.WorkspaceID, input)
	if err != nil {
		return nil, err
	}
	if input.SecretKey == "" {
		return nil, ErrBackupBucket
	}
	server, err := s.servers.secretServer(actor.WorkspaceID, input.Server)
	if err != nil {
		return nil, err
	}
	_, err = s.apps.DefaultBucket(actor.WorkspaceID)
	first := errors.Is(err, repository.ErrNoBucket)
	if err != nil && !first {
		return nil, err
	}
	bucket := model.Bucket{
		ID:          uuid.New().String(),
		WorkspaceID: actor.WorkspaceID,
		Name:        clean.Name,
		IsDefault:   input.Default || first,
		Endpoint:    clean.Endpoint,
		Region:      clean.Region,
		Bucket:      clean.Bucket,
		Path:        clean.Path,
		AccessKey:   clean.AccessKey,
		CreatedAt:   s.now().UTC(),
	}
	if _, err := s.apps.Bucket(actor.WorkspaceID, bucket.Name); err == nil {
		return nil, repository.ErrDuplicateBucket
	}
	err = s.keepBucketKey(ctx, server, bucket, strings.TrimSpace(input.SecretKey))
	if err != nil {
		return nil, err
	}
	err = s.apps.CreateBucket(bucket)
	if err != nil {
		return nil, err
	}
	s.configureDaemons(actor.WorkspaceID)
	s.servers.record(actor, "bucket.added", "bucket", bucket.Name, map[string]string{"bucket": bucket.Bucket, "endpoint": bucket.Endpoint, "path": bucket.Path, "server": server.Name})
	if bucket.IsDefault && !first {
		s.servers.record(actor, "bucket.default", "bucket", bucket.Name, nil)
	}
	s.notify(actor.WorkspaceID)
	return s.Bucket(actor.WorkspaceID, bucket.Name)
}

// keepBucketKey has a server write a test file to the bucket with
// secretKey, or with the key it keeps when secretKey is empty, and keep
// the key.
func (s *AppService) keepBucketKey(ctx context.Context, server *model.Server, bucket model.Bucket, secretKey string) error {
	ctx, cancel := context.WithTimeout(ctx, secretTimeout)
	defer cancel()
	err := s.servers.Request(ctx, server.ID, protocol.TypeBucketSet, protocol.BucketSet{Bucket: *daemonBucket(&bucket), SecretKey: secretKey}, nil)
	if errors.Is(err, ErrServerOffline) {
		return &OfflineError{Server: server.Name}
	}
	if err != nil {
		return err
	}
	return s.servers.servers.SetServerSecret(server.ID, protocol.BucketSecret(bucket.ID), false, s.now().UTC())
}

// EditBucket changes a bucket: its name, where it is, its keys, or makes
// it the default. A new place or new keys are checked first by a server,
// which keeps the key; apps then start there with a full copy, and the old
// place keeps what it has. A new access key or secret key leaves the other
// servers without a key: enter it for them again.
func (s *AppService) EditBucket(ctx context.Context, actor Actor, name string, input BucketInput) (*Bucket, error) {
	stored, err := s.apps.Bucket(actor.WorkspaceID, name)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) == "" {
		input.Name = stored.Name
	}
	clean, err := cleanBucket(actor.WorkspaceID, input)
	if err != nil {
		return nil, err
	}
	secretKey := strings.TrimSpace(input.SecretKey)
	newKey := clean.AccessKey != stored.AccessKey || secretKey != ""
	moved := newKey || clean.Endpoint != stored.Endpoint || clean.Region != stored.Region || clean.Bucket != stored.Bucket || clean.Path != stored.Path
	bucket := *stored
	bucket.Name, bucket.Endpoint, bucket.Region, bucket.Bucket, bucket.Path, bucket.AccessKey =
		clean.Name, clean.Endpoint, clean.Region, clean.Bucket, clean.Path, clean.AccessKey
	var server *model.Server
	if moved {
		server, err = s.servers.secretServer(actor.WorkspaceID, input.Server)
		if err != nil {
			return nil, err
		}
		if err := s.keepBucketKey(ctx, server, bucket, secretKey); err != nil {
			return nil, err
		}
		if newKey {
			s.dropBucketKeyElsewhere(ctx, actor.WorkspaceID, bucket.ID, server.ID)
		}
	}
	becameDefault := input.Default && !stored.IsDefault
	bucket.IsDefault = stored.IsDefault || input.Default
	if !moved && !becameDefault && bucket.Name == stored.Name {
		return s.Bucket(actor.WorkspaceID, bucket.Name)
	}
	err = s.apps.UpdateBucket(bucket)
	if err != nil {
		return nil, err
	}
	s.configureDaemons(actor.WorkspaceID)
	if moved || bucket.Name != stored.Name {
		details := map[string]string{"bucket": bucket.Bucket, "endpoint": bucket.Endpoint, "path": bucket.Path}
		if bucket.Name != stored.Name {
			details["from"] = stored.Name
		}
		if server != nil {
			details["server"] = server.Name
		}
		s.servers.record(actor, "bucket.changed", "bucket", bucket.Name, details)
	}
	if becameDefault {
		s.servers.record(actor, "bucket.default", "bucket", bucket.Name, nil)
	}
	s.notify(actor.WorkspaceID)
	return s.Bucket(actor.WorkspaceID, bucket.Name)
}

// SetBucketKey hands one more server the secret key of a bucket, such as
// a server added after the bucket: it checks the bucket with it first.
func (s *AppService) SetBucketKey(ctx context.Context, actor Actor, name, serverName, secretKey string) (*Bucket, error) {
	bucket, err := s.apps.Bucket(actor.WorkspaceID, name)
	if err != nil {
		return nil, err
	}
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return nil, ErrBackupBucket
	}
	server, err := s.servers.secretServer(actor.WorkspaceID, serverName)
	if err != nil {
		return nil, err
	}
	if err := s.keepBucketKey(ctx, server, *bucket, secretKey); err != nil {
		return nil, err
	}
	s.reconfigure(server.ID, actor.WorkspaceID)
	s.servers.record(actor, "bucket.key_set", "bucket", bucket.Name, map[string]string{"server": server.Name})
	s.notify(actor.WorkspaceID)
	return s.Bucket(actor.WorkspaceID, bucket.Name)
}

// dropBucketKeyElsewhere has every server but keep forget the key of a
// bucket whose keys change: the old key is no use to them.
func (s *AppService) dropBucketKeyElsewhere(ctx context.Context, workspaceID, bucketID, keep string) {
	index, err := s.servers.secretsIndex(workspaceID)
	if err != nil {
		return
	}
	for _, server := range index.servers {
		if server.ID == keep || !index.has(server.ID, protocol.BucketSecret(bucketID)) {
			continue
		}
		if s.servers.Online(server.ID) {
			if err := s.servers.setSecret(ctx, &server, protocol.BucketSecret(bucketID), ""); err == nil {
				continue
			}
		}
		_ = s.servers.servers.SetServerSecret(server.ID, protocol.BucketSecret(bucketID), true, s.now().UTC())
	}
}

// MakeDefaultBucket makes a bucket the workspace's default, as it is.
func (s *AppService) MakeDefaultBucket(ctx context.Context, actor Actor, name string) (*Bucket, error) {
	stored, err := s.apps.Bucket(actor.WorkspaceID, name)
	if err != nil {
		return nil, err
	}
	return s.EditBucket(ctx, actor, name, BucketInput{Name: stored.Name, Endpoint: stored.Endpoint, Region: stored.Region, Bucket: stored.Bucket, Path: stored.Path, AccessKey: stored.AccessKey, Default: true})
}

// RemoveBucket forgets a bucket. One that apps use, or that keeps older
// backups of an app, stays: BucketInUseError says which. What is in the
// bucket stays either way. confirm is the bucket's name, typed out.
func (s *AppService) RemoveBucket(actor Actor, name, confirm string) error {
	bucket, err := s.apps.Bucket(actor.WorkspaceID, name)
	if err != nil {
		return err
	}
	if err := confirmed(confirm, bucket.Name); err != nil {
		return err
	}
	apps, older, err := s.apps.BucketApps(actor.WorkspaceID, bucket.ID)
	if err != nil {
		return err
	}
	if len(apps) > 0 || len(older) > 0 {
		return &BucketInUseError{Bucket: bucket.Name, Apps: apps, Older: older}
	}
	err = s.apps.DeleteBucket(actor.WorkspaceID, bucket.ID)
	if err != nil {
		return err
	}
	s.configureDaemons(actor.WorkspaceID)
	s.servers.dropSecret(context.Background(), actor.WorkspaceID, protocol.BucketSecret(bucket.ID))
	s.servers.record(actor, "bucket.removed", "bucket", bucket.Name, nil)
	s.notify(actor.WorkspaceID)
	return nil
}

// SetAppBucket moves an app's new backups to a bucket of the
// workspace; an empty name follows the workspace's default. Its older
// backups stay where they are, and restores of them read from there.
func (s *AppService) SetAppBucket(actor Actor, idOrName, name string) (*model.App, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	var to *string
	if name != "" {
		bucket, err := s.apps.Bucket(actor.WorkspaceID, name)
		if err != nil {
			return nil, err
		}
		to = &bucket.ID
	} else if _, err := s.apps.DefaultBucket(actor.WorkspaceID); err != nil {
		if errors.Is(err, repository.ErrNoBucket) {
			return nil, ErrNoBackups
		}
		return nil, err
	}
	if (to == nil && app.BucketID == nil) || (to != nil && app.BucketID != nil && *to == *app.BucketID) {
		return app, nil
	}
	from := ""
	if current, err := s.appBucket(app); err == nil {
		from = current.ID
	}
	err = s.apps.SetAppBucket(app.ID, to, from, s.now().UTC())
	if err != nil {
		return nil, err
	}
	s.configureDaemons(actor.WorkspaceID)
	s.servers.record(actor, "app.bucket_set", "app", app.Name, map[string]string{"bucket": name})
	s.notify(actor.WorkspaceID)
	return s.apps.ByID(app.ID)
}

// cleanBucket trims what people typed and checks the name, the endpoint
// and the required fields. The secret key is not part of it: AddBucket
// needs it, EditBucket may keep the one the servers have.
func cleanBucket(workspaceID string, input BucketInput) (protocol.Bucket, error) {
	bucket := protocol.Bucket{
		Name:      strings.TrimSpace(input.Name),
		Endpoint:  strings.TrimRight(strings.TrimSpace(input.Endpoint), "/"),
		Region:    strings.TrimSpace(input.Region),
		Bucket:    strings.TrimSpace(input.Bucket),
		Path:      strings.Trim(strings.TrimSpace(input.Path), "/"),
		Workspace: workspaceID,
		AccessKey: strings.TrimSpace(input.AccessKey),
	}
	if !protocol.ValidAppName(bucket.Name) {
		return bucket, ErrBucketName
	}
	if bucket.Endpoint != "" {
		parsed, err := url.Parse(bucket.Endpoint)
		local := parsed != nil && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1")
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && local)) {
			return bucket, ErrBackupEndpoint
		}
	}
	if bucket.Bucket == "" || bucket.AccessKey == "" {
		return bucket, ErrBackupBucket
	}
	if bucket.Region == "" {
		bucket.Region = "auto"
	}
	return bucket, nil
}

// daemonBucket is a bucket as the daemon needs it: the server fills in
// the secret key it keeps.
func daemonBucket(bucket *model.Bucket) *protocol.Bucket {
	return &protocol.Bucket{
		ID:        bucket.ID,
		Endpoint:  bucket.Endpoint,
		Region:    bucket.Region,
		Bucket:    bucket.Bucket,
		Path:      bucket.Path,
		Workspace: bucket.WorkspaceID,
		AccessKey: bucket.AccessKey,
		Name:      bucket.Name,
	}
}
