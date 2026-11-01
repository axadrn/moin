package agent

import (
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
//	<path>/<app>/<database>/litestream/   every change, for a point in time restore
//	<path>/<app>/<database>/snapshots/    full copies: before deploys, changes, restores
//
// Once a day the agent restores the newest state into a temp file and runs
// the integrity check on it, so "backed up" means "restored and verified".
const (
	keepSnapshots    = 50
	pointInTime      = 7 * 24 * time.Hour
	drillEvery       = 24 * time.Hour
	firstDrill       = 2 * time.Minute
	snapshotTimeout  = 10 * time.Minute
	snapshotIDLayout = "20060102T150405Z"
)

// backups replicates every database of the running apps and answers the
// backup requests of the control plane.
type backups struct {
	apps *apps
	send func(protocol.MessageType, string, any)

	mu       sync.Mutex
	storage  *protocol.BackupStorage
	store    *litestream.Store
	watched  map[string]watched
	verified map[string]drill
}

// watched is one replicated database.
type watched struct {
	app      string
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

// configure sets the bucket and replicates every database to it. Nil
// stops replication.
func (b *backups) configure(ctx context.Context, storage *protocol.BackupStorage) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.storage != nil && storage != nil && *b.storage == *storage {
		return nil
	}
	b.stopLocked()
	b.storage = storage
	if storage == nil {
		return nil
	}
	b.resetForTarget()
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

// resetForTarget clears Litestream's local state next to every database
// when backups go somewhere new. That state tracks what the old target
// holds; against a new, empty one Litestream would wait for files it
// already cleaned up. The new target starts with a full snapshot.
func (b *backups) resetForTarget() {
	target := b.storage.Endpoint + "|" + b.storage.Bucket + "|" + b.storage.Path
	file := filepath.Join(filepath.Dir(b.apps.dir), "backup-target")
	previous, _ := os.ReadFile(file)
	if string(previous) == target {
		return
	}
	entries, _ := os.ReadDir(b.apps.dir)
	for _, entry := range entries {
		for _, database := range b.apps.runningDatabases(entry.Name()) {
			_ = os.RemoveAll(filepath.Join(filepath.Dir(database.HostPath), "."+filepath.Base(database.HostPath)+litestream.MetaDirSuffix))
		}
	}
	_ = os.WriteFile(file, []byte(target), 0o600)
}

func (b *backups) stopLocked() {
	if b.store != nil {
		_ = b.store.Close(context.Background())
	}
	b.store = nil
	b.watched = map[string]watched{}
}

// reconcile replicates the databases of the running releases: new ones
// start, gone ones stop. Deploys and restores call it. A database that is
// slow to open never holds up the others or a backup request.
func (b *backups) reconcile() {
	b.mu.Lock()
	b.reconcileLocked()
	b.mu.Unlock()
}

func (b *backups) reconcileLocked() {
	store := b.store
	if store == nil {
		return
	}
	want := map[string]watched{}
	entries, _ := os.ReadDir(b.apps.dir)
	for _, entry := range entries {
		for _, database := range b.apps.runningDatabases(entry.Name()) {
			want[database.HostPath] = watched{app: entry.Name(), database: database}
		}
	}
	for hostPath := range b.watched {
		if _, ok := want[hostPath]; !ok {
			go store.UnregisterDB(context.Background(), hostPath)
			delete(b.watched, hostPath)
		}
	}
	for hostPath, item := range want {
		if _, ok := b.watched[hostPath]; ok {
			continue
		}
		b.watched[hostPath] = item
		db := litestream.NewDB(hostPath)
		replica := litestream.NewReplicaWithClient(db, b.replicaClient(item.app, item.database.Path))
		db.Replica = replica
		go func() {
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

func (b *backups) prefix(app, database string) string {
	return strings.Trim(path.Join(b.storage.Path, app, databaseKey(database)), "/")
}

func (b *backups) replicaClient(app, database string) *lss3.ReplicaClient {
	client := lss3.NewReplicaClient()
	client.AccessKeyID = b.storage.AccessKey
	client.SecretAccessKey = b.storage.SecretKey
	client.Region = b.storage.Region
	client.Bucket = b.storage.Bucket
	client.Endpoint = b.storage.Endpoint
	client.ForcePathStyle = b.storage.Endpoint != ""
	client.Path = b.prefix(app, database) + "/litestream"
	return client
}

func (b *backups) s3Client() *s3.Client {
	storage := b.storage
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
	if !appNamePattern.MatchString(app) {
		return nil, errors.New("invalid app name")
	}
	all := b.apps.runningDatabases(app)
	if database == "" {
		if len(all) == 0 {
			return nil, fmt.Errorf("%s has no SQLite database on a volume", app)
		}
		return all, nil
	}
	for _, found := range all {
		if found.Path == database {
			return []protocol.Database{found}, nil
		}
	}
	return nil, fmt.Errorf("%s has no database %s", app, database)
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
	return b.list(ctx, protocol.BackupRequest{App: request.App, Database: request.Database})
}

// snapshotOne uploads one snapshot and drops the oldest beyond the limit.
func (b *backups) snapshotOne(ctx context.Context, app string, database protocol.Database, reason string) (protocol.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	copyPath := database.HostPath + ".ops-snapshot"
	_ = os.Remove(copyPath)
	defer os.Remove(copyPath)
	// Read-only, but without query_only, which forbids VACUUM INTO.
	db, err := sql.Open("sqlite", "file:"+database.HostPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return protocol.Snapshot{}, err
	}
	_, err = db.ExecContext(ctx, `VACUUM INTO `+quoteText(copyPath))
	db.Close()
	if err != nil {
		return protocol.Snapshot{}, err
	}
	gzipped, err := gzipFile(copyPath)
	if err != nil {
		return protocol.Snapshot{}, err
	}
	defer os.Remove(gzipped)
	file, err := os.Open(gzipped)
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
	b.mu.Lock()
	client, bucket, key := b.s3Client(), b.storage.Bucket, b.prefix(app, database.Path)+"/snapshots/"+snapshot.ID+".db.gz"
	b.mu.Unlock()
	_, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: file, ContentLength: aws.Int64(info.Size())})
	if err != nil {
		return protocol.Snapshot{}, fmt.Errorf("upload: %w", err)
	}
	snapshots, err := b.snapshots(ctx, app, database.Path)
	if err == nil && len(snapshots) > keepSnapshots {
		for _, old := range snapshots[keepSnapshots:] {
			_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(b.prefix(app, database.Path) + "/snapshots/" + old.ID + ".db.gz")})
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
	client, bucket, prefix := b.s3Client(), b.storage.Bucket, b.prefix(app, database)+"/snapshots/"
	b.mu.Unlock()
	snapshots := []protocol.Snapshot{}
	pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			id := strings.TrimSuffix(strings.TrimPrefix(aws.ToString(object.Key), prefix), ".db.gz")
			stamp, reason, _ := strings.Cut(id, "-")
			created, err := time.Parse(snapshotIDLayout, stamp)
			if err != nil {
				continue
			}
			snapshots = append(snapshots, protocol.Snapshot{ID: id, Reason: reason, Size: aws.ToInt64(object.Size), CreatedAt: created})
		}
	}
	slices.SortFunc(snapshots, func(x, y protocol.Snapshot) int { return y.CreatedAt.Compare(x.CreatedAt) })
	return snapshots, nil
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
		result.Databases = append(result.Databases, b.status(ctx, request.App, database, true))
	}
	return result, nil
}

// status is how one database is backed up. withSnapshots also lists its
// snapshots, which reads the bucket.
func (b *backups) status(ctx context.Context, app string, database protocol.Database, withSnapshots bool) protocol.DatabaseBackup {
	backup := protocol.DatabaseBackup{Database: database.Path, Snapshots: []protocol.Snapshot{}}
	if withSnapshots {
		snapshots, err := b.snapshots(ctx, app, database.Path)
		if err != nil {
			backup.Error = "list snapshots: " + err.Error()
		}
		if snapshots != nil {
			backup.Snapshots = snapshots
		}
	}
	b.mu.Lock()
	client := b.replicaClient(app, database.Path)
	last := b.verified[database.HostPath]
	b.mu.Unlock()
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
	defer b.apps.lock(request.App)()

	restored := database.HostPath + ".ops-restore"
	removeDatabase(restored)
	defer removeDatabase(restored)
	if request.Snapshot != "" {
		err = b.download(ctx, request.App, database.Path, request.Snapshot, restored)
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
	client := b.replicaClient(request.App, database.Path)
	b.mu.Unlock()
	if swapErr == nil {
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

func (b *backups) download(ctx context.Context, app, database, id, target string) error {
	b.mu.Lock()
	client, bucket, key := b.s3Client(), b.storage.Bucket, b.prefix(app, database)+"/snapshots/"+id+".db.gz"
	b.mu.Unlock()
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
	client := b.replicaClient(app, database)
	b.mu.Unlock()
	options := litestream.NewRestoreOptions()
	options.OutputPath = target
	options.Timestamp = at
	err := litestream.NewReplicaWithClient(nil, client).Restore(ctx, options)
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
	_ = os.RemoveAll(filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+litestream.MetaDirSuffix))
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
func (b *backups) drills(ctx context.Context) {
	timer := time.NewTimer(firstDrill)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		b.drillAll(ctx)
		timer.Reset(drillEvery)
	}
}

func (b *backups) drillAll(ctx context.Context) {
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
		result := drill{at: time.Now().UTC()}
		if err != nil {
			result.err = "restore drill failed: " + err.Error()
			slog.Error("backup: restore drill failed", "app", item.app, "database", item.database.Path, "error", err)
		}
		b.verified[item.database.HostPath] = result
		b.mu.Unlock()
		matchOwner(item.database.HostPath)
		reports[item.app] = append(reports[item.app], b.status(ctx, item.app, item.database, false))
	}
	for app, databases := range reports {
		b.send(protocol.TypeBackupStatus, "", protocol.Backups{App: app, Databases: databases})
	}
}

func (b *backups) drillOne(ctx context.Context, item watched) error {
	target := filepath.Join(os.TempDir(), "ops-drill-"+item.app+"-"+databaseKey(item.database.Path))
	removeDatabase(target)
	defer removeDatabase(target)
	b.mu.Lock()
	client := b.replicaClient(item.app, item.database.Path)
	b.mu.Unlock()
	options := litestream.NewRestoreOptions()
	options.OutputPath = target
	err := litestream.NewReplicaWithClient(nil, client).Restore(ctx, options)
	if err != nil {
		return err
	}
	return integrityCheck(ctx, target)
}
