package daemon

import (
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/benbjohnson/litestream"
	lss3 "github.com/benbjohnson/litestream/s3"

	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/protocol"
)

// sqliteEngine backs up a SQLite file with Litestream, inside the daemon:
// it writes every change into the bucket. Everything else is Litestream's
// own restore into a file next to the database (restoreAt):
//
//	going back     a minute, the file becomes the database in one switch
//	daily check    the newest state, into a throwaway file
//	download       the newest state, gzipped into the bucket
//
// What moin puts next to app.db all starts with app.db.moin- or .moin-:
// app.db.moin-restore (being built), app.db.moin-before-<id> (an undo),
// .moin-temp-* (a check or a download).
type sqliteEngine struct {
	b        *backups
	app      string
	database protocol.Database
}

// spareRoom is what stays free on the disk on top of what a copy, a check
// or a restore needs.
const spareRoom = 512 << 20

// room says when the disk of dir has less free than need plus spareRoom.
func room(dir string, need int64) error {
	var stat syscall.Statfs_t
	if syscall.Statfs(dir, &stat) != nil {
		return nil
	}
	if free := int64(stat.Bavail) * int64(stat.Bsize); free < need+spareRoom {
		return fmt.Errorf("the server's disk has %s free and this needs %s: free some space", human.Size(float64(free)), human.Size(float64(need+spareRoom)))
	}
	return nil
}

func fileSize(name string) int64 {
	info, err := os.Stat(name)
	if err != nil {
		return 0
	}
	return info.Size()
}

// Restore builds app.db.moin-restore, or takes the undo
// app.db.moin-before-<undo>. With the app stopped and the live file's log
// folded in, it links the live file as app.db.moin-before-<id>, the undo,
// and renames the built file over it: one atomic step, the old file
// before it, the new one after, never none.
func (e *sqliteEngine) Restore(ctx context.Context, from source, id string) error {
	live := e.database.HostPath
	aside, built := undoFile(live, id), live+".moin-restore"
	if from.undo != "" {
		built = undoFile(live, from.undo)
		if !exists(built) {
			return fmt.Errorf("there is no state before the restore %s: it is older than the app keeps", from.undo)
		}
	} else {
		removeDatabase(built)
		defer removeDatabase(built)
		if err := e.restoreAt(ctx, from.at, built); err != nil {
			return fmt.Errorf("%w; nothing changed", err)
		}
	}
	info, err := os.Stat(live)
	if err != nil || exists(aside) {
		return errors.New("the live file is missing or an undo has its name; nothing changed")
	}
	if err := errors.Join(os.Chmod(built, info.Mode().Perm()), syncFile(built)); err != nil {
		return err
	}
	matchOwnerOf(built, info)
	start, err := e.b.apps.stopServices(ctx, e.app)
	if err != nil {
		return err
	}
	e.b.unwatch(live)
	if err := checkpoint(ctx, live); err != nil {
		return errors.Join(fmt.Errorf("fold the log into the database: %w; nothing changed", err), start())
	}
	if err := errors.Join(os.Link(live, aside), syncFile(filepath.Dir(live))); err != nil {
		os.Remove(aside)
		return errors.Join(fmt.Errorf("keep the undo: %w; nothing changed", err), start())
	}
	if err := os.Rename(built, live); err != nil {
		os.Remove(aside)
		return errors.Join(fmt.Errorf("switch: %w; nothing changed", err), start())
	}
	_ = syncFile(filepath.Dir(live))
	// Every change starts again from the restored file.
	_ = os.RemoveAll(litestreamState(live))
	if err := start(); err != nil {
		return fmt.Errorf("the restored data is in place, but %w", err)
	}
	return nil
}

func undoFile(live, id string) string { return live + ".moin-before-" + id }

// restoreAt has Litestream write the database as it was at at into file,
// the newest state when at is zero, and runs the integrity check on it.
func (e *sqliteEngine) restoreAt(ctx context.Context, at time.Time, file string) error {
	live := e.database.HostPath
	if err := room(filepath.Dir(live), fileSize(live)); err != nil {
		return err
	}
	client, err := e.b.replica(e.app, e.database)
	if err != nil {
		return err
	}
	options := litestream.NewRestoreOptions()
	options.OutputPath, options.Timestamp = file, at
	err = litestream.NewReplicaWithClient(nil, client).Restore(ctx, options)
	if errors.Is(err, litestream.ErrTxNotAvailable) || errors.Is(err, litestream.ErrNoSnapshots) {
		return fmt.Errorf("Every change of %s has nothing for that time", e.database.Path)
	}
	if err != nil {
		return err
	}
	if err := integrityCheck(ctx, file); err != nil {
		return fmt.Errorf("what came back failed the integrity check: %w", err)
	}
	return nil
}

// restoreNewest restores the newest state into a throwaway file next to
// the database. The second result removes the file; it is nil on an error.
func (e *sqliteEngine) restoreNewest(ctx context.Context) (string, func(), error) {
	dir, err := os.MkdirTemp(filepath.Dir(e.database.HostPath), "."+tempName("temp-*"))
	if err != nil {
		return "", nil, err
	}
	remove := func() { os.RemoveAll(dir) }
	file := filepath.Join(dir, "newest.db")
	if err := e.restoreAt(ctx, time.Time{}, file); err != nil {
		remove()
		return "", nil, err
	}
	return file, remove, nil
}

// Check restores the newest state into a throwaway file, checks it and
// deletes it.
func (e *sqliteEngine) Check(ctx context.Context) error {
	_, remove, err := e.restoreNewest(ctx)
	if err != nil {
		return err
	}
	remove()
	return nil
}

// download restores the newest state of a SQLite file into a temp file,
// gzips it into the bucket under downloads/, removes the temp file and
// says which download it is.
func (b *backups) download(ctx context.Context, request protocol.BackupRequest) (protocol.Copy, error) {
	database, err := b.one(request.App, request.Database)
	if err != nil {
		return protocol.Copy{}, err
	}
	bucket, folder, err := b.where(request.App, database)
	if err != nil {
		return protocol.Copy{}, err
	}
	e := &sqliteEngine{b: b, app: request.App, database: database}
	file, remove, err := e.restoreNewest(ctx)
	if err != nil {
		return protocol.Copy{}, err
	}
	defer remove()
	in, err := os.Open(file)
	if err != nil {
		return protocol.Copy{}, err
	}
	defer in.Close()
	at := time.Now().UTC()
	id := protocol.NewCopyID(at)
	err = upload(ctx, bucket, protocol.DownloadFile(folder, id), nil, func(ctx context.Context, w io.Writer) error {
		zipped := gzip.NewWriter(w)
		_, err := io.Copy(zipped, in)
		return errors.Join(err, zipped.Close())
	})
	if err != nil {
		return protocol.Copy{}, fmt.Errorf("the download of %s: %w", database.Path, err)
	}
	return protocol.Copy{ID: id, At: at}, nil
}

// Recover tells from the undo whether the switch happened. It also
// removes what was half built.
func (e *sqliteEngine) Recover(ctx context.Context, mark restoreMark) (bool, error) {
	live := e.database.HostPath
	aside := undoFile(live, mark.Undo)
	if mark.From == "" {
		defer removeDatabase(live + ".moin-restore")
	}
	asideInfo, err := os.Stat(aside)
	if err != nil {
		// No undo: the switch never began.
		return false, nil
	}
	liveInfo, err := os.Stat(live)
	if err != nil {
		// No live file: the undo is the only one, it goes back.
		return false, os.Rename(aside, live)
	}
	if os.SameFile(liveInfo, asideInfo) {
		// A second name of the live file: the switch never happened.
		os.Remove(aside)
		return false, nil
	}
	// Another file: the switch happened.
	return true, os.RemoveAll(litestreamState(live))
}

func (e *sqliteEngine) Undos(ctx context.Context) ([]protocol.Copy, error) {
	files, err := filepath.Glob(undoFile(e.database.HostPath, "*"))
	undos := []protocol.Copy{}
	for index := len(files) - 1; index >= 0; index-- {
		id := strings.TrimPrefix(files[index], undoFile(e.database.HostPath, ""))
		at, ok := protocol.CopyTime(id)
		if info, statErr := os.Stat(files[index]); ok && statErr == nil {
			undos = append(undos, protocol.Copy{ID: id, At: at, Size: info.Size()})
		}
	}
	return undos, err
}

func (e *sqliteEngine) Drop(ctx context.Context, id string) error {
	removeDatabase(undoFile(e.database.HostPath, id))
	return nil
}

// sweepTemp removes the checks and downloads a stopped daemon left next
// to a database.
func sweepTemp(live string) {
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(live), "."+tempName("temp-*")))
	for _, dir := range left {
		_ = os.RemoveAll(dir)
	}
}

// watched is a file Litestream follows into folder, kept keep days.
type watched struct {
	bucket protocol.Bucket
	folder string
	keep   int
	db     *litestream.DB
}

// follow has Litestream write every change of a file into
// litestream/<generation>/ beside its folder. The generation is written in
// Litestream's folder next to the file: without that folder, as after a
// restore, a new generation starts, and the old one in the bucket ages
// out (pruneGenerations). When Litestream reports trouble, it says so.
func (b *backups) follow(ctx context.Context, app string, database protocol.Database) error {
	live := database.HostPath
	if generation(live) == "" {
		if err := startGeneration(live); err != nil {
			return err
		}
	}
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return err
	}
	want := watched{bucket: *bucket, folder: folder + "/litestream/" + generation(live), keep: b.schedule(app).Keep}
	b.mu.Lock()
	now, known := b.watched[live]
	b.mu.Unlock()
	if known && now.bucket == want.bucket && now.folder == want.folder && now.keep == want.keep {
		if why := now.db.SyncDiagnostic().Error; why != "" {
			return errors.New("writing down every change: " + why)
		}
		return nil
	}
	b.unwatch(live)
	return b.register(ctx, live, want)
}

// letGo has Litestream let go of the SQLite files of an app; the next round
// follows them again. After a deploy, Litestream starts over: the app that
// started may have left its log different from what Litestream last read.
// The generation stays, so the history is kept.
func (b *backups) letGo(app string) {
	for _, database := range b.apps.liveDatabases(app) {
		if !database.Postgres() {
			b.unwatch(database.HostPath)
		}
	}
}

// storeFor is the Litestream store that keeps files for keep days.
func (b *backups) storeFor(ctx context.Context, keep int) (*litestream.Store, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if store := b.stores[keep]; store != nil {
		return store, nil
	}
	store := litestream.NewStore(nil, litestream.DefaultCompactionLevels)
	store.SnapshotRetention = time.Duration(keep) * day
	if err := store.Open(ctx); err != nil {
		return nil, err
	}
	b.stores[keep] = store
	return store, nil
}

// register has Litestream start following a file.
func (b *backups) register(ctx context.Context, live string, want watched) error {
	store, err := b.storeFor(ctx, want.keep)
	if err != nil {
		return fmt.Errorf("writing down every change: %w", err)
	}
	want.db = litestream.NewDB(live)
	want.db.Replica = litestream.NewReplicaWithClient(want.db, replicaClient(&want.bucket, want.folder))
	// A bucket that lost its place starts over instead of waiting forever.
	want.db.Replica.AutoRecoverEnabled = true
	if err := store.RegisterDB(want.db); err != nil {
		return fmt.Errorf("writing down every change: %w", err)
	}
	matchOwner(live)
	b.mu.Lock()
	b.watched[live] = want
	b.mu.Unlock()
	return nil
}

// pruneSQLite deletes the undos and the generations older than each app
// keeps, but nothing of an app the hub sent no schedule for.
func (b *backups) pruneSQLite(ctx context.Context) {
	for app, databases := range b.running() {
		b.mu.Lock()
		_, scheduled := b.schedules[app]
		b.mu.Unlock()
		if !scheduled {
			continue
		}
		before := time.Now().Add(-time.Duration(b.schedule(app).Keep) * day)
		for _, database := range databases {
			if database.Postgres() {
				continue
			}
			err := errors.Join(b.pruneGenerations(ctx, app, database, before), b.dropUndos(ctx, app, database, before))
			if err != nil {
				slog.Warn("backup: delete what is older than the app keeps", "app", app, "database", database.Path, "error", err)
			}
		}
	}
}

// pruneGenerations deletes the generations of Every change but the one
// the file is on, once nothing in them is newer than before.
func (b *backups) pruneGenerations(ctx context.Context, app string, database protocol.Database, before time.Time) error {
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return err
	}
	client, current := s3Client(bucket), folder+"/litestream/"+generation(database.HostPath)+"/"
	for _, old := range folders(ctx, client, bucket.Bucket, folder+"/litestream/") {
		var objects []types.Object
		pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket.Bucket), Prefix: aws.String(old)})
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				return err
			}
			objects = append(objects, page.Contents...)
		}
		if old == current || slices.ContainsFunc(objects, func(object types.Object) bool { return object.LastModified.After(before) }) {
			continue
		}
		for _, object := range objects {
			if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket.Bucket), Key: object.Key}); err != nil {
				return err
			}
		}
	}
	return nil
}

// generation is the generation of Every change a file is on, "" without
// one.
func generation(live string) string {
	body, _ := os.ReadFile(generationFile(live))
	return strings.TrimSpace(string(body))
}

// startGeneration begins a new generation of Every change for a file.
func startGeneration(live string) error {
	if err := os.RemoveAll(litestreamState(live)); err != nil {
		return err
	}
	if err := os.MkdirAll(litestreamState(live), 0o700); err != nil {
		return err
	}
	return os.WriteFile(generationFile(live), []byte(protocol.NewCopyID(time.Now())), 0o600)
}

func generationFile(live string) string {
	return filepath.Join(litestreamState(live), tempName("generation"))
}

// replica is where the generation of a file is in the bucket.
func (b *backups) replica(app string, database protocol.Database) (*lss3.ReplicaClient, error) {
	bucket, folder, err := b.where(app, database)
	if err == nil && generation(database.HostPath) == "" {
		err = errors.New("Every change has written nothing down yet")
	}
	if err != nil {
		return nil, err
	}
	return replicaClient(bucket, folder+"/litestream/"+generation(database.HostPath)), nil
}

// unwatch has Litestream let go of a file, with what was written last.
func (b *backups) unwatch(live string) {
	b.mu.Lock()
	old, known := b.watched[live]
	delete(b.watched, live)
	store := b.stores[old.keep]
	b.mu.Unlock()
	if known && store != nil {
		_ = store.UnregisterDB(context.Background(), live)
	}
}

// forget lets go of the files that do not run any more.
func (b *backups) forget(running map[string][]protocol.Database) {
	want := map[string]bool{}
	for _, databases := range running {
		for _, database := range databases {
			want[database.HostPath] = true
		}
	}
	b.mu.Lock()
	var gone []string
	for live := range b.watched {
		if !want[live] {
			gone = append(gone, live)
		}
	}
	b.mu.Unlock()
	for _, live := range gone {
		b.unwatch(live)
	}
}

// reach is how far back Every change of a file reaches, and its newest
// change.
func (b *backups) reach(ctx context.Context, app string, database protocol.Database) (since, until *time.Time) {
	client, err := b.replica(app, database)
	if err != nil {
		return nil, nil
	}
	first, last, err := litestream.NewReplicaWithClient(nil, client).TimeBounds(ctx)
	if err != nil || first.IsZero() {
		return nil, nil
	}
	return &first, &last
}

func replicaClient(bucket *protocol.Bucket, folder string) *lss3.ReplicaClient {
	client := lss3.NewReplicaClient()
	client.AccessKeyID, client.SecretAccessKey = bucket.AccessKey, bucket.SecretKey
	client.Region, client.Bucket, client.Endpoint = bucket.Region, bucket.Bucket, bucket.Endpoint
	client.ForcePathStyle = bucket.Endpoint != ""
	client.Path = folder
	return client
}

// litestreamState is the folder Litestream keeps next to a database.
func litestreamState(live string) string {
	return filepath.Join(filepath.Dir(live), "."+filepath.Base(live)+litestream.MetaDirSuffix)
}

// checkpoint folds the log of a SQLite file into it and removes the log.
func checkpoint(ctx context.Context, live string) error {
	db, err := sql.Open("sqlite", "file:"+live+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	var busy, logged, folded int
	err = db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logged, &folded)
	db.Close()
	if err == nil && busy != 0 {
		err = errors.New("the database is still in use")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err == nil {
			if err = os.Remove(live + suffix); errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
	}
	return err
}

// backupFile says a name is one moin leaves next to a database.
func backupFile(name string) bool { return strings.Contains(name, "."+tempName("")) }

func removeDatabase(name string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(name + suffix)
	}
}

func integrityCheck(ctx context.Context, file string) error {
	db, err := sql.Open("sqlite", "file:"+file+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return errors.New(result)
	}
	return nil
}
