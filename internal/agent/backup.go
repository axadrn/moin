package agent

import (
	"cmp"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/benbjohnson/litestream"
	lss3 "github.com/benbjohnson/litestream/s3"

	"github.com/axadrn/ops/protocol"
)

// Backups run inside the agent, two ways, into the user's bucket:
//
//	<path>/<workspace>/<app>/<app ID>/<database>/litestream/   every change, for a point in time restore
//	<path>/<workspace>/<app>/<app ID>/<database>/snapshots/    full copies: before deploys, changes, restores
//
// The workspace ID keeps workspaces that share a bucket apart, the app ID
// an app from an earlier one of the same name.
//
// Once a day the agent restores the newest state into a temp file and runs
// the integrity check on it, so "backed up" means "restored and verified".
const (
	keepSnapshots    = 50
	pointInTime      = 7 * 24 * time.Hour
	drillEvery       = 24 * time.Hour
	firstDrill       = 2 * time.Minute
	snapshotTimeout  = 10 * time.Minute
	snapshotIDLayout = protocol.SnapshotIDLayout
	keepUploads      = 24 * time.Hour
	sweepEvery       = time.Hour
)

// backups replicates every database of the running apps and answers the
// backup requests of the control plane.
type backups struct {
	apps *apps
	send func(protocol.MessageType, string, any)

	mu sync.Mutex
	// storage is the workspace's default bucket, appStorage the bucket of
	// each app that uses another one.
	storage    *protocol.BackupStorage
	appStorage map[string]*protocol.BackupStorage
	store      *litestream.Store
	watched    map[string]watched
	verified   map[string]drill
	// walg runs the continuous Postgres backups.
	walg walg
}

// watched is one replicated database; prefix is its folder in the bucket.
type watched struct {
	app      string
	storage  protocol.BackupStorage
	prefix   string
	database protocol.Database
}

// drill is the newest restore drill of a database.
type drill struct {
	at  time.Time
	err string
}

func newBackups(a *apps, send func(protocol.MessageType, string, any)) *backups {
	return &backups{apps: a, send: send, watched: map[string]watched{}, verified: map[string]drill{}}
}

// configure sets the default bucket and the buckets of single apps, and
// replicates every database to its app's bucket. A nil storage stops
// replication.
func (b *backups) configure(ctx context.Context, storage *protocol.BackupStorage, appStorage map[string]*protocol.BackupStorage) error {
	for _, other := range appStorage {
		if other == nil || other.Workspace == "" {
			return errors.New("start backups: a bucket has no workspace folder, update the control plane")
		}
	}
	b.mu.Lock()
	if b.storage != nil && storage != nil && *b.storage == *storage {
		// The same default: only apps that changed buckets start again.
		if sameStorage(b.appStorage, appStorage) {
			b.mu.Unlock()
			return nil
		}
		b.appStorage = appStorage
		store, gone := b.store, b.reconcileLocked()
		b.mu.Unlock()
		for _, hostPath := range gone {
			_ = store.UnregisterDB(context.Background(), hostPath)
		}
		return nil
	}
	defer b.mu.Unlock()
	b.stopLocked()
	b.storage, b.appStorage = storage, appStorage
	if storage == nil {
		return nil
	}
	if storage.Workspace == "" {
		b.storage = nil
		return errors.New("start backups: the bucket has no workspace folder, update the control plane")
	}
	b.store = litestream.NewStore(nil, litestream.DefaultCompactionLevels)
	b.store.SnapshotRetention = pointInTime
	b.store.Logger = slog.Default().With("part", "backup")
	err := b.store.Open(ctx)
	if err != nil {
		b.store = nil
		return fmt.Errorf("start backups: %w", err)
	}
	b.reconcileLocked()
	return nil
}

// moved reports whether a database's backups go somewhere new: another
// bucket, folder or app ID. backup-targets.json remembers where each
// database went. Litestream's local state next to a moved database must
// go: it tracks what the old place holds, and against a new, empty one
// Litestream would wait for files it already cleaned up. The new place
// starts with a full snapshot.
func (b *backups) moved(hostPath string, storage protocol.BackupStorage, prefix string) bool {
	file := filepath.Join(filepath.Dir(b.apps.dir), "backup-targets.json")
	targets := map[string]string{}
	body, _ := os.ReadFile(file)
	_ = json.Unmarshal(body, &targets)
	target := storage.Endpoint + "|" + storage.Bucket + "|" + prefix
	if targets[hostPath] == target {
		return false
	}
	targets[hostPath] = target
	saveJSON(file, targets)
	return true
}

// litestreamState is the folder Litestream keeps next to a database.
func litestreamState(hostPath string) string {
	return filepath.Join(filepath.Dir(hostPath), "."+filepath.Base(hostPath)+litestream.MetaDirSuffix)
}

func (b *backups) stopLocked() {
	if b.store != nil {
		_ = b.store.Close(context.Background())
	}
	b.store = nil
	b.watched = map[string]watched{}
}

// reconcile replicates the databases of the running releases: new ones
// start, gone ones stop. Deploys, restores and removals call it. A
// database that is slow to open never holds up the others or a backup
// request. Gone ones stop before it returns, so an app created again
// right after starts on a clean slate.
func (b *backups) reconcile() {
	b.mu.Lock()
	store, gone := b.store, b.reconcileLocked()
	b.mu.Unlock()
	for _, hostPath := range gone {
		_ = store.UnregisterDB(context.Background(), hostPath)
	}
}

// reconcileLocked starts the new databases and returns the gone ones for
// the caller to stop.
func (b *backups) reconcileLocked() (gone []string) {
	store := b.store
	if store == nil {
		return nil
	}
	want := map[string]watched{}
	entries, _ := os.ReadDir(b.apps.dir)
	for _, entry := range entries {
		for _, database := range b.apps.runningDatabases(entry.Name()) {
			if database.Postgres() {
				// Postgres has snapshots, no Litestream.
				continue
			}
			prefix, err := b.prefix(entry.Name(), database.Path)
			if err != nil {
				slog.Warn("backup: not replicated", "app", entry.Name(), "database", database.Path, "error", err)
				continue
			}
			want[database.HostPath] = watched{app: entry.Name(), storage: *b.storageFor(entry.Name()), prefix: prefix, database: database}
		}
	}
	for hostPath := range b.watched {
		if _, ok := want[hostPath]; !ok {
			gone = append(gone, hostPath)
			delete(b.watched, hostPath)
		}
	}
	for hostPath, item := range want {
		old, known := b.watched[hostPath]
		if known && old.prefix == item.prefix && old.storage == item.storage {
			continue
		}
		b.watched[hostPath] = item
		moved := b.moved(hostPath, item.storage, item.prefix)
		db := litestream.NewDB(hostPath)
		replica := litestream.NewReplicaWithClient(db, replicaClientAt(&item.storage, item.prefix))
		db.Replica = replica
		go func() {
			// The same file under a new app ID: the old replica stops
			// first, or the store would keep it.
			if known {
				_ = store.UnregisterDB(context.Background(), hostPath)
			}
			if moved {
				_ = os.RemoveAll(litestreamState(hostPath))
			}
			err := store.RegisterDB(db)
			if err != nil {
				slog.Error("backup: replicate failed", "app", item.app, "database", item.database.Path, "error", err)
				b.mu.Lock()
				if b.store == store {
					delete(b.watched, hostPath)
				}
				b.mu.Unlock()
				return
			}
			matchOwner(hostPath)
		}()
	}
	return gone
}

// runningDatabases are the databases of an app's running release.
func (a *apps) runningDatabases(app string) []protocol.Database {
	current := a.current(app)
	if current == 0 {
		return nil
	}
	var detected protocol.Detected
	body, _ := os.ReadFile(filepath.Join(a.releaseDir(app, current), "detected.json"))
	_ = json.Unmarshal(body, &detected)
	return detected.Databases
}

// databaseKey is a database's folder in the bucket: its path in the
// container without slashes, like data-app.db.
func databaseKey(database string) string {
	return strings.ReplaceAll(strings.Trim(database, "/"), "/", "-")
}

// prefix is a database's folder in the bucket:
// <path>/<workspace>/<app>/<app ID>/<database>. An app deployed before
// deploys brought the app ID has none until its next deploy.
func (b *backups) prefix(app, database string) (string, error) {
	return b.prefixIn(b.storageFor(app), app, database)
}

// prefixIn is a database's folder in another bucket, one the app left.
func (b *backups) prefixIn(storage *protocol.BackupStorage, app, database string) (string, error) {
	id := b.apps.appID(app)
	if id == "" {
		return "", fmt.Errorf("deploy %s once more to start its backups", app)
	}
	return strings.Trim(path.Join(storage.Path, storage.Workspace, app, id, databaseKey(database)), "/"), nil
}

// storageFor is the bucket of an app: its own, or the default. The caller
// holds the lock.
func (b *backups) storageFor(app string) *protocol.BackupStorage {
	if storage := b.appStorage[app]; storage != nil {
		return storage
	}
	return b.storage
}

// sameStorage reports whether two sets of app buckets are the same.
func sameStorage(one, other map[string]*protocol.BackupStorage) bool {
	if len(one) != len(other) {
		return false
	}
	for app, storage := range one {
		if theirs := other[app]; theirs == nil || storage == nil || *theirs != *storage {
			return false
		}
	}
	return true
}

// replicaClient is where Litestream keeps every change of a database.
func (b *backups) replicaClient(app, database string) (*lss3.ReplicaClient, error) {
	prefix, err := b.prefix(app, database)
	if err != nil {
		return nil, err
	}
	return replicaClientAt(b.storageFor(app), prefix), nil
}

func replicaClientAt(storage *protocol.BackupStorage, prefix string) *lss3.ReplicaClient {
	client := lss3.NewReplicaClient()
	client.AccessKeyID = storage.AccessKey
	client.SecretAccessKey = storage.SecretKey
	client.Region = storage.Region
	client.Bucket = storage.Bucket
	client.Endpoint = storage.Endpoint
	client.ForcePathStyle = storage.Endpoint != ""
	client.Path = prefix + "/litestream"
	return client
}

func s3Client(storage *protocol.BackupStorage) *s3.Client {
	region := storage.Region
	if region == "" {
		region = "auto"
	}
	return s3.New(s3.Options{
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider(storage.AccessKey, storage.SecretKey, ""),
		BaseEndpoint: aws.String(endpointOrAWS(storage)),
		UsePathStyle: storage.Endpoint != "",
	})
}

func endpointOrAWS(storage *protocol.BackupStorage) string {
	if storage.Endpoint != "" {
		return storage.Endpoint
	}
	region := storage.Region
	if region == "" {
		region = "us-east-1"
	}
	return "https://s3." + region + ".amazonaws.com"
}

// ready returns the bucket, or an error people can act on.
func (b *backups) ready() (*protocol.BackupStorage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.storage == nil {
		return nil, errors.New("backups are not set up: add a bucket on the Backups tab of an app")
	}
	storage := *b.storage
	return &storage, nil
}

// databases returns the app's running databases, or the one named.
func (b *backups) databases(app, database string) ([]protocol.Database, error) {
	if !protocol.ValidAppName(app) {
		return nil, errors.New("invalid app name")
	}
	all := b.apps.runningDatabases(app)
	if database == "" {
		if len(all) == 0 {
			return nil, fmt.Errorf("no database in %s: no SQLite file on a volume, no Postgres service", app)
		}
		return all, nil
	}
	for _, found := range all {
		if found.Path == database {
			return []protocol.Database{found}, nil
		}
	}
	return nil, fmt.Errorf("no database %s in %s", database, app)
}

// snapshot copies every database of an app with VACUUM INTO, gzips it and
// uploads it. A reason names it, such as "manual" or "before-v4".
func (b *backups) snapshot(ctx context.Context, request protocol.BackupRequest) (protocol.Backups, error) {
	if _, err := b.ready(); err != nil {
		return protocol.Backups{}, err
	}
	databases, err := b.databases(request.App, request.Database)
	if err != nil {
		return protocol.Backups{}, err
	}
	reason := request.Reason
	if reason == "" {
		reason = "manual"
	}
	for _, database := range databases {
		_, err = b.snapshotOne(ctx, request.App, database, reason)
		if err != nil {
			return protocol.Backups{}, fmt.Errorf("snapshot %s: %w", database.Path, err)
		}
	}
	return b.list(ctx, protocol.BackupRequest{App: request.App, Database: request.Database, Older: request.Older})
}

// snapshotOne uploads one snapshot and drops the oldest beyond the limit.
func (b *backups) snapshotOne(ctx context.Context, app string, database protocol.Database, reason string) (protocol.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	var copied string
	var cleanup func()
	var err error
	if database.Postgres() {
		copied, cleanup, err = b.pgCopy(ctx, app, database)
	} else {
		copied, cleanup, err = gzippedCopy(ctx, database.HostPath)
	}
	if err != nil {
		return protocol.Snapshot{}, err
	}
	defer cleanup()
	file, err := os.Open(copied)
	if err != nil {
		return protocol.Snapshot{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return protocol.Snapshot{}, err
	}
	now := time.Now().UTC()
	snapshot := protocol.Snapshot{ID: now.Format(snapshotIDLayout) + "-" + cleanReason(reason), Reason: cleanReason(reason), Size: info.Size(), CreatedAt: now}
	// Changes in the same second, like a few SQL writes in a row, each keep
	// their own snapshot instead of replacing the first one.
	snapshots, listErr := b.snapshots(ctx, app, database.Path)
	taken := func(id string) bool {
		return slices.ContainsFunc(snapshots, func(other protocol.Snapshot) bool { return other.ID == id })
	}
	for n := 2; listErr == nil && taken(snapshot.ID); n++ {
		snapshot.Reason = fmt.Sprintf("%s-%d", cleanReason(reason), n)
		snapshot.ID = now.Format(snapshotIDLayout) + "-" + snapshot.Reason
	}
	b.mu.Lock()
	storage := b.storageFor(app)
	client, bucket := s3Client(storage), storage.Bucket
	snapshot.Bucket = storage.Name
	prefix, err := b.prefix(app, database.Path)
	b.mu.Unlock()
	if err != nil {
		return protocol.Snapshot{}, err
	}
	key := prefix + "/snapshots/" + snapshot.ID + snapshotExt(database)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: file, ContentLength: aws.Int64(info.Size())})
	if err != nil {
		return protocol.Snapshot{}, fmt.Errorf("upload: %w", err)
	}
	snapshots = append([]protocol.Snapshot{snapshot}, snapshots...)
	if listErr == nil && len(snapshots) > keepSnapshots {
		for _, old := range snapshots[keepSnapshots:] {
			_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(prefix + "/snapshots/" + old.ID + snapshotExt(database))})
		}
	}
	slog.Info("backup: snapshot", "app", app, "database", database.Path, "id", snapshot.ID, "size", snapshot.Size)
	return snapshot, nil
}

// snapshotBefore takes a snapshot of every database of a running app
// before a change, when backups are set up. It logs what it did.
func (b *backups) snapshotBefore(ctx context.Context, app, reason string, logf func(string, ...any)) error {
	if _, err := b.ready(); err != nil {
		return nil
	}
	for _, database := range b.apps.runningDatabases(app) {
		snapshot, err := b.snapshotOne(ctx, app, database, reason)
		if errors.Is(err, errPostgresDown) {
			// A Postgres that does not run holds nothing a change could
			// break, and a deploy may be what fixes it: go on, and say so.
			if logf != nil {
				logf("no snapshot of %s: it is not running", database.Path)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("snapshot of %s before the change: %w", database.Path, err)
		}
		if logf != nil {
			logf("snapshot of %s: %s", database.Path, snapshot.ID)
		}
	}
	return nil
}

func cleanReason(reason string) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return '-'
	}, reason)
	return strings.Trim(clean, "-")
}

func quoteText(text string) string { return `'` + strings.ReplaceAll(text, `'`, `''`) + `'` }

// snapshotFolderPrefix starts the folder a snapshot copies a database to.
const snapshotFolderPrefix = ".ops-snapshot-"

// gzippedCopy copies a database with VACUUM INTO and gzips the copy. Each
// copy gets its own folder next to the database, so two snapshots of one
// database at the same time do not remove each other's files. cleanup
// removes the folder.
func gzippedCopy(ctx context.Context, hostPath string) (string, func(), error) {
	dir, err := os.MkdirTemp(filepath.Dir(hostPath), snapshotFolderPrefix+"*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	copyPath := filepath.Join(dir, filepath.Base(hostPath))
	// Read-only, but without query_only, which forbids VACUUM INTO.
	db, err := sql.Open("sqlite", "file:"+hostPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		cleanup()
		return "", nil, err
	}
	_, err = db.ExecContext(ctx, `VACUUM INTO `+quoteText(copyPath))
	db.Close()
	if err != nil {
		cleanup()
		return "", nil, err
	}
	gzipped, err := gzipFile(copyPath)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return gzipped, cleanup, nil
}

func gzipFile(source string) (string, error) {
	in, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer in.Close()
	target := source + ".gz"
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	writer := gzip.NewWriter(out)
	_, err = io.Copy(writer, in)
	if err == nil {
		err = writer.Close()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(target)
		return "", err
	}
	return target, nil
}

// snapshots lists the snapshots of a database, newest first.
func (b *backups) snapshots(ctx context.Context, app, database string) ([]protocol.Snapshot, error) {
	b.mu.Lock()
	storage := b.storageFor(app)
	b.mu.Unlock()
	if storage == nil {
		return nil, errors.New("backups are not set up")
	}
	return b.snapshotsIn(ctx, storage, app, database)
}

// snapshotsIn lists the snapshots of a database in one bucket, newest
// first, each with the bucket's name.
func (b *backups) snapshotsIn(ctx context.Context, storage *protocol.BackupStorage, app, database string) ([]protocol.Snapshot, error) {
	client, bucket := s3Client(storage), storage.Bucket
	folder, err := b.prefixIn(storage, app, database)
	if err != nil {
		return nil, err
	}
	prefix := folder + "/snapshots/"
	snapshots := []protocol.Snapshot{}
	pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			id := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(aws.ToString(object.Key), prefix), ".db.gz"), pgSnapshotExt)
			stamp, reason, _ := strings.Cut(id, "-")
			created, err := time.Parse(snapshotIDLayout, stamp)
			if err != nil {
				continue
			}
			snapshots = append(snapshots, protocol.Snapshot{ID: id, Reason: reason, Size: aws.ToInt64(object.Size), CreatedAt: created, Bucket: storage.Name})
		}
	}
	sortSnapshots(snapshots)
	return snapshots, nil
}

// sortSnapshots puts the newest first; in one second the numbered ones
// like before-sql-2 came after the first.
func sortSnapshots(snapshots []protocol.Snapshot) {
	slices.SortFunc(snapshots, func(x, y protocol.Snapshot) int {
		return cmp.Or(y.CreatedAt.Compare(x.CreatedAt), cmp.Compare(len(y.ID), len(x.ID)), strings.Compare(y.ID, x.ID))
	})
}

// list reports the backups of an app: snapshots, the point in time window
// and the newest drill of each database.
func (b *backups) list(ctx context.Context, request protocol.BackupRequest) (protocol.Backups, error) {
	result := protocol.Backups{App: request.App, Databases: []protocol.DatabaseBackup{}}
	if _, err := b.ready(); err != nil {
		return result, err
	}
	databases, err := b.databases(request.App, request.Database)
	if err != nil {
		return result, err
	}
	for _, database := range databases {
		backup := b.status(ctx, request.App, database, true)
		// The buckets the app left keep its older snapshots; restores
		// read them from there.
		for _, older := range request.Older {
			snapshots, err := b.snapshotsIn(ctx, &older, request.App, database.Path)
			if err != nil {
				slog.Warn("backup: list older snapshots", "app", request.App, "bucket", older.Name, "error", err)
				continue
			}
			backup.Snapshots = append(backup.Snapshots, snapshots...)
		}
		sortSnapshots(backup.Snapshots)
		result.Databases = append(result.Databases, backup)
	}
	return result, nil
}

// status is how one database is backed up. withSnapshots also lists its
// snapshots, which reads the bucket.
func (b *backups) status(ctx context.Context, app string, database protocol.Database, withSnapshots bool) protocol.DatabaseBackup {
	backup := protocol.DatabaseBackup{Database: database.Path, Kind: database.Kind, Snapshots: []protocol.Snapshot{}}
	if withSnapshots {
		snapshots, err := b.snapshots(ctx, app, database.Path)
		if err != nil {
			backup.Error = "list snapshots: " + err.Error()
		}
		if snapshots != nil {
			backup.Snapshots = snapshots
		}
	}
	if database.Postgres() {
		// The point in time window comes from WAL-G's backups.
		since, until, err := b.walgWindow(ctx, app, database)
		if err == nil && !since.IsZero() {
			backup.Since, backup.ReplicatedAt = &since, &until
		}
		backup.Note = b.walgNote(app, database)
		b.mu.Lock()
		last := b.verified[drillKey(app, database)]
		b.mu.Unlock()
		if !last.at.IsZero() {
			at := last.at
			backup.VerifiedAt = &at
			if last.err != "" && backup.Error == "" {
				backup.Error = last.err
			}
		}
		return backup
	}
	b.mu.Lock()
	client, err := b.replicaClient(app, database.Path)
	last := b.verified[database.HostPath]
	b.mu.Unlock()
	if err != nil {
		backup.Error = err.Error()
		return backup
	}
	since, until, err := litestream.NewReplicaWithClient(nil, client).TimeBounds(ctx)
	if err == nil && !since.IsZero() {
		backup.Since, backup.ReplicatedAt = &since, &until
	}
	if !last.at.IsZero() {
		at := last.at
		backup.VerifiedAt = &at
		if last.err != "" && backup.Error == "" {
			backup.Error = last.err
		}
	}
	return backup
}

// restore puts a database back from a snapshot or a point in time. It
// takes a snapshot of the current state first, checks the restored file,
// stops the app for the swap and starts it again.
func (b *backups) restore(ctx context.Context, request protocol.BackupRestore) error {
	if _, err := b.ready(); err != nil {
		return err
	}
	databases, err := b.databases(request.App, request.Database)
	if err != nil {
		return err
	}
	if len(databases) > 1 {
		return errors.New("the app has several databases: name the one to restore")
	}
	database := databases[0]
	if request.Snapshot == "" && request.At == nil {
		return errors.New("choose a snapshot or a point in time")
	}
	if database.Postgres() {
		return b.pgRestore(ctx, request, database)
	}
	defer b.apps.lock(request.App)()

	restored := database.HostPath + ".ops-restore"
	removeDatabase(restored)
	defer removeDatabase(restored)
	if request.Snapshot != "" {
		err = b.download(ctx, request.Storage, request.App, database.Path, request.Snapshot, restored)
	} else {
		err = b.restorePoint(ctx, request.App, database.Path, *request.At, restored)
	}
	if err != nil {
		return err
	}
	err = integrityCheck(ctx, restored)
	if err != nil {
		return fmt.Errorf("the backup failed the integrity check: %w", err)
	}
	_, err = b.snapshotOne(ctx, request.App, database, "before-restore")
	if err != nil {
		return fmt.Errorf("snapshot before the restore: %w", err)
	}

	composeFile := filepath.Join(b.apps.releaseDir(request.App, b.apps.current(request.App)), "compose.json")
	noLog := func(string, ...any) {}
	err = b.apps.compose(ctx, request.App, composeFile, noLog, "stop")
	if err != nil {
		return fmt.Errorf("stop the app: %w", err)
	}
	b.mu.Lock()
	if b.store != nil {
		_ = b.store.UnregisterDB(ctx, database.HostPath)
		delete(b.watched, database.HostPath)
	}
	b.mu.Unlock()
	swapErr := swapDatabase(restored, database.HostPath)
	// The point in time history belongs to the old database; it starts
	// again from the restored one.
	b.mu.Lock()
	client, clientErr := b.replicaClient(request.App, database.Path)
	b.mu.Unlock()
	if swapErr == nil && clientErr == nil {
		if err := client.DeleteAll(ctx); err != nil {
			slog.Warn("backup: clear the old replica", "error", err)
		}
	}
	startErr := b.apps.compose(ctx, request.App, composeFile, noLog, "start")
	b.reconcile()
	if swapErr != nil {
		return fmt.Errorf("swap the database: %w", swapErr)
	}
	if startErr != nil {
		return fmt.Errorf("start the app again: %w", startErr)
	}
	slog.Info("backup: restored", "app", request.App, "database", database.Path, "snapshot", request.Snapshot, "at", request.At)
	return nil
}

// download fetches a snapshot from the app's bucket, or from older, a
// bucket the app left.
func (b *backups) download(ctx context.Context, older *protocol.BackupStorage, app, database, id, target string) error {
	b.mu.Lock()
	storage := b.storageFor(app)
	if older != nil {
		storage = older
	}
	client, bucket := s3Client(storage), storage.Bucket
	prefix, err := b.prefixIn(storage, app, database)
	b.mu.Unlock()
	if err != nil {
		return err
	}
	key := prefix + "/snapshots/" + id + ".db.gz"
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	var missing *types.NoSuchKey
	if errors.As(err, &missing) {
		return fmt.Errorf("there is no snapshot %s of %s", id, database)
	}
	if err != nil {
		return fmt.Errorf("snapshot %s: %w", id, err)
	}
	defer object.Body.Close()
	reader, err := gzip.NewReader(object.Body)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, reader)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

// restorePoint restores the replica as it was at a point in time.
func (b *backups) restorePoint(ctx context.Context, app, database string, at time.Time, target string) error {
	b.mu.Lock()
	client, err := b.replicaClient(app, database)
	b.mu.Unlock()
	if err != nil {
		return err
	}
	options := litestream.NewRestoreOptions()
	options.OutputPath = target
	options.Timestamp = at
	err = litestream.NewReplicaWithClient(nil, client).Restore(ctx, options)
	if errors.Is(err, litestream.ErrTxNotAvailable) {
		return fmt.Errorf("%s UTC is outside the point in time window of %s: pick a time in the window the backups show, or a snapshot", at.UTC().Format(time.DateTime), database)
	}
	if err != nil {
		return fmt.Errorf("restore to %s: %w", at.Format(time.RFC3339), err)
	}
	return nil
}

// swapDatabase moves the restored file into place with the owner and mode
// of the database it replaces.
func swapDatabase(restored, target string) error {
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	err = os.Chmod(restored, info.Mode().Perm())
	if err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_ = os.Remove(target + suffix)
		_ = os.Remove(restored + suffix)
	}
	// Litestream keeps its position in the old file next to it; the new
	// file starts over.
	_ = os.RemoveAll(litestreamState(target))
	err = os.Rename(restored, target)
	if err != nil {
		return err
	}
	matchOwnerOf(target, info)
	return nil
}

// removeDatabase removes a database file with its -wal and -shm files.
func removeDatabase(path string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(path + suffix)
	}
}

func integrityCheck(ctx context.Context, file string) error {
	db, err := sql.Open("sqlite", "file:"+file+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result)
	if err != nil {
		return err
	}
	if result != "ok" {
		return errors.New(result)
	}
	return nil
}

// drills restores the newest state of every replicated database into a
// temp file once a day and checks it, then reports to the control plane.
// While a database has nothing in the bucket yet, it tries again soon.
func (b *backups) drills(ctx context.Context) {
	timer := time.NewTimer(firstDrill)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if b.drillAll(ctx) {
			timer.Reset(firstDrill)
		} else {
			timer.Reset(drillEvery)
		}
	}
}

// drillAll drills every database. waiting reports a database with nothing
// replicated yet: it has no drill, neither passed nor failed.
func (b *backups) drillAll(ctx context.Context) (waiting bool) {
	b.mu.Lock()
	items := make([]watched, 0, len(b.watched))
	for _, item := range b.watched {
		items = append(items, item)
	}
	b.mu.Unlock()
	reports := map[string][]protocol.DatabaseBackup{}
	for _, item := range items {
		err := b.drillOne(ctx, item)
		b.mu.Lock()
		if errors.Is(err, litestream.ErrTxNotAvailable) {
			waiting = true
			delete(b.verified, item.database.HostPath)
		} else {
			result := drill{at: time.Now().UTC()}
			if err != nil {
				result.err = "restore drill failed: " + err.Error()
				slog.Error("backup: restore drill failed", "app", item.app, "database", item.database.Path, "error", err)
			}
			b.verified[item.database.HostPath] = result
		}
		b.mu.Unlock()
		matchOwner(item.database.HostPath)
		reports[item.app] = append(reports[item.app], b.status(ctx, item.app, item.database, false))
	}
	if b.pgDrills(ctx, reports) {
		waiting = true
	}
	for app, databases := range reports {
		b.send(protocol.TypeBackupStatus, "", protocol.Backups{App: app, Databases: databases})
	}
	return waiting
}

func (b *backups) drillOne(ctx context.Context, item watched) error {
	target := filepath.Join(os.TempDir(), "ops-drill-"+item.app+"-"+databaseKey(item.database.Path))
	removeDatabase(target)
	defer removeDatabase(target)
	b.mu.Lock()
	client := replicaClientAt(&item.storage, item.prefix)
	b.mu.Unlock()
	options := litestream.NewRestoreOptions()
	options.OutputPath = target
	err := litestream.NewReplicaWithClient(nil, client).Restore(ctx, options)
	if err != nil {
		return err
	}
	return integrityCheck(ctx, target)
}

// sweepUploads deletes, once an hour, what ops deploy left in the bucket:
// a source the server never fetched, because the CLI stopped after the
// upload or the deploy never started. The server deletes each source as
// soon as it has it; this catches the rest after a day.
func (b *backups) sweepUploads(ctx context.Context) {
	ticker := time.NewTicker(sweepEvery)
	defer ticker.Stop()
	for {
		b.sweepOnce(ctx, time.Now().Add(-keepUploads))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sweepOnce deletes the uploads of the workspace older than before, under
// <path>/<workspace>/<app>/<app ID>/uploads/, also those of removed apps,
// in the default bucket and in every bucket of an app.
func (b *backups) sweepOnce(ctx context.Context, before time.Time) {
	b.mu.Lock()
	if b.storage == nil {
		b.mu.Unlock()
		return
	}
	storages := []protocol.BackupStorage{*b.storage}
	for _, storage := range b.appStorage {
		if !slices.ContainsFunc(storages, func(known protocol.BackupStorage) bool {
			return known.Endpoint == storage.Endpoint && known.Bucket == storage.Bucket && known.Path == storage.Path
		}) {
			storages = append(storages, *storage)
		}
	}
	b.mu.Unlock()
	for _, storage := range storages {
		sweepIn(ctx, &storage, before)
	}
}

// sweepIn deletes the old uploads of the workspace in one bucket.
func sweepIn(ctx context.Context, storage *protocol.BackupStorage, before time.Time) {
	client, bucket := s3Client(storage), storage.Bucket
	root := strings.TrimLeft(path.Join(storage.Path, storage.Workspace)+"/", "/")
	folders := func(prefix string) []string {
		var found []string
		pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix), Delimiter: aws.String("/")})
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				slog.Warn("backup: sweep uploads", "prefix", prefix, "error", err)
				return found
			}
			for _, folder := range page.CommonPrefixes {
				found = append(found, aws.ToString(folder.Prefix))
			}
		}
		return found
	}
	for _, app := range folders(root) {
		for _, id := range folders(app) {
			pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(id + "uploads/")})
			for pages.HasMorePages() {
				page, err := pages.NextPage(ctx)
				if err != nil {
					slog.Warn("backup: sweep uploads", "prefix", id, "error", err)
					break
				}
				for _, object := range page.Contents {
					if object.LastModified == nil || !object.LastModified.Before(before) {
						continue
					}
					_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: object.Key})
					if err != nil {
						slog.Warn("backup: delete old upload", "key", aws.ToString(object.Key), "error", err)
					} else {
						slog.Info("backup: deleted an upload nobody fetched", "key", aws.ToString(object.Key))
					}
				}
			}
		}
	}
}
