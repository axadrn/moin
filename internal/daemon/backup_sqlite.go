package daemon

import (
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
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

// sqliteEngine backs up a SQLite file. A copy is VACUUM INTO, SQLite's
// own clean copy while the app writes, into a temp file next to the
// database, gzipped into the bucket, the temp file deleted at once. With
// Every change, Litestream, inside the daemon, also writes each change
// into the bucket, so a restore can go back to any minute.
//
// What moin puts next to app.db all starts with app.db.moin- or .moin-:
// app.db.moin-restore (being built), app.db.moin-before-<id> (an undo),
// .moin-copy-* and .moin-check-* (a copy or a check being made).
type sqliteEngine struct {
	b        *backups
	app      string
	database protocol.Database
}

func (e *sqliteEngine) Copy(ctx context.Context, w io.Writer) error {
	dir, err := e.tempDir("copy")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "copy.db")
	db, err := sql.Open("sqlite", "file:"+e.database.HostPath+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `VACUUM INTO '`+strings.ReplaceAll(file, `'`, `''`)+`'`)
	db.Close()
	if err != nil {
		return err
	}
	in, err := os.Open(file)
	if err != nil {
		return err
	}
	defer in.Close()
	zipped := gzip.NewWriter(w)
	_, err = io.Copy(zipped, in)
	return errors.Join(err, zipped.Close())
}

// tempDir is a folder next to the database for a copy or a check, once
// the disk has room for one more database.
func (e *sqliteEngine) tempDir(what string) (string, error) {
	dir := filepath.Dir(e.database.HostPath)
	if err := room(dir, fileSize(e.database.HostPath)); err != nil {
		return "", err
	}
	return os.MkdirTemp(dir, "."+tempName(what+"-*"))
}

// room says when the disk of dir has less free than need, plus 512 MB.
func room(dir string, need int64) error {
	var stat syscall.Statfs_t
	if syscall.Statfs(dir, &stat) != nil {
		return nil
	}
	if free := int64(stat.Bavail) * int64(stat.Bsize); free < need+512<<20 {
		return fmt.Errorf("the server's disk has %s free and this needs %s: free some space", human.Size(float64(free)), human.Size(float64(need+512<<20)))
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

// Restore builds app.db.moin-restore and checks it, or takes the undo
// app.db.moin-before-<undo>. With the app stopped it folds the live
// file's log into it, renames it to app.db.moin-before-<id> and the built
// file in its place.
func (e *sqliteEngine) Restore(ctx context.Context, from source, id string, step func(string) error) (err error) {
	live := e.database.HostPath
	aside, built := undoFile(live, id), live+".moin-restore"
	if from.undo != "" {
		built = undoFile(live, from.undo)
		if !exists(built) {
			return fmt.Errorf("there is no state before the restore %s: it is older than the app keeps", from.undo)
		}
	} else {
		if err := room(filepath.Dir(live), fileSize(live)); err != nil {
			return fmt.Errorf("%w; nothing changed", err)
		}
		removeDatabase(built)
		defer func() {
			// After a crash the crash rule needs it (and tests crash here).
			if !errors.Is(err, errCrash) {
				removeDatabase(built)
			}
		}()
		if err := e.build(ctx, from, built); err != nil {
			return fmt.Errorf("%w; nothing changed", err)
		}
		if err := integrityCheck(ctx, built); err != nil {
			return fmt.Errorf("what came back failed the integrity check, nothing changed: %w", err)
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
	if err := step("switching"); err != nil {
		return err
	}
	if err := os.Rename(live, aside); err != nil {
		return errors.Join(fmt.Errorf("switch: %w; nothing changed", err), start())
	}
	if err := step("aside"); err != nil {
		return err
	}
	if err := os.Rename(built, live); err != nil {
		return errors.Join(fmt.Errorf("switch: %w", err), os.Rename(aside, live), start())
	}
	_ = syncFile(filepath.Dir(live))
	if err := step("switched"); err != nil {
		return err
	}
	// Every change starts again from the restored file.
	_ = os.RemoveAll(litestreamState(live))
	if err := start(); err != nil {
		return fmt.Errorf("the restored data is in place, but %w", err)
	}
	return nil
}

func undoFile(live, id string) string { return live + ".moin-before-" + id }

// build writes the database of a copy, or of a minute, into target.
func (e *sqliteEngine) build(ctx context.Context, from source, target string) error {
	if from.copy == nil {
		client, err := e.b.replica(e.app, e.database)
		if err != nil {
			return err
		}
		options := litestream.NewRestoreOptions()
		options.OutputPath, options.Timestamp = target, from.at
		err = litestream.NewReplicaWithClient(nil, client).Restore(ctx, options)
		if errors.Is(err, litestream.ErrTxNotAvailable) || errors.Is(err, litestream.ErrNoSnapshots) {
			return fmt.Errorf("Every change of %s does not reach %s", e.database.Path, from.at.Format("Jan 2 15:04 UTC"))
		}
		return err
	}
	unzipped, err := gzip.NewReader(from.copy)
	if err != nil {
		return fmt.Errorf("the copy is not gzipped: %w", err)
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, unzipped)
	return errors.Join(err, out.Close())
}

// Recover puts the file back by the crash rule (recoverFrom): a file in
// the live place that is not the restored one goes aside as an undo.
func (e *sqliteEngine) Recover(ctx context.Context, mark restoreMark) (bool, error) {
	live := e.database.HostPath
	aside, built := undoFile(live, mark.Undo), live+".moin-restore"
	if mark.From != "" {
		built = undoFile(live, mark.From)
	}
	// No live file is never switched: the built one is what would be there.
	switched, err := recoverFrom(mark.Step, exists(aside), exists(built) || !exists(live), func() error {
		if exists(live) {
			if err := os.Rename(live, undoFile(live, strayID(mark.Undo))); err != nil {
				return err
			}
		}
		return errors.Join(os.Rename(aside, live), syncFile(filepath.Dir(live)))
	})
	if err != nil {
		return false, err
	}
	removeDatabase(live + ".moin-restore")
	if switched {
		err = os.RemoveAll(litestreamState(live))
	}
	return switched, err
}

// Check unpacks a copy next to the database and runs SQLite's integrity
// check, which reads every page.
func (e *sqliteEngine) Check(ctx context.Context, copy io.Reader) error {
	dir, err := e.tempDir("check")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "check.db")
	if err := e.build(ctx, source{copy: copy}, file); err != nil {
		return err
	}
	return integrityCheck(ctx, file)
}

func (e *sqliteEngine) Undos(ctx context.Context) ([]protocol.Copy, error) {
	files, err := filepath.Glob(undoFile(e.database.HostPath, "*"))
	undos := []protocol.Copy{}
	for index := len(files) - 1; index >= 0; index-- {
		id := strings.TrimPrefix(files[index], undoFile(e.database.HostPath, ""))
		at, parseErr := time.Parse(protocol.CopyIDLayout, id)
		if info, statErr := os.Stat(files[index]); parseErr == nil && statErr == nil {
			undos = append(undos, protocol.Copy{ID: id, At: at, Size: info.Size()})
		}
	}
	return undos, err
}

func (e *sqliteEngine) Drop(ctx context.Context, id string) error {
	removeDatabase(undoFile(e.database.HostPath, id))
	return nil
}

// sweepTemp removes the copies and checks a stopped daemon left next to
// a database.
func sweepTemp(live string) {
	for _, what := range []string{"copy", "check"} {
		left, _ := filepath.Glob(filepath.Join(filepath.Dir(live), "."+tempName(what+"-*")))
		for _, dir := range left {
			_ = os.RemoveAll(dir)
		}
	}
}

// watched is a file Litestream follows into folder, kept keep days.
type watched struct {
	bucket protocol.Bucket
	folder string
	keep   int
	db     *litestream.DB
}

// record has Litestream follow a file with Every change, into
// litestream/<generation>/ next to its copies. The generation is written
// in Litestream's folder next to the file: without that folder, as after
// a restore, a new generation starts, and the old one in the bucket
// ages out with the copies (prune). Nothing in the bucket is deleted
// here, but when Every change is turned off.
func (b *backups) record(ctx context.Context, app string, database protocol.Database, schedule protocol.Schedule) error {
	live := database.HostPath
	if !schedule.EveryChange {
		b.unwatch(live)
		if client, err := b.replica(app, database); err == nil && exists(litestreamState(live)) {
			return errors.Join(client.DeleteAll(ctx), os.RemoveAll(litestreamState(live)))
		}
		return nil
	}
	if generation(live) == "" {
		if err := errors.Join(os.RemoveAll(litestreamState(live)), os.MkdirAll(litestreamState(live), 0o700), os.WriteFile(generationFile(live), []byte(newCopyID()), 0o600)); err != nil {
			return err
		}
	}
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return err
	}
	folder += "/litestream/" + generation(live)
	b.mu.Lock()
	old, known := b.watched[live]
	b.mu.Unlock()
	if known && old.bucket == *bucket && old.folder == folder && old.keep == schedule.Keep {
		if why := old.db.SyncDiagnostic().Error; why != "" {
			return errors.New("writing down every change: " + why)
		}
		return nil
	}
	b.unwatch(live)
	b.mu.Lock()
	store := b.stores[schedule.Keep]
	if store == nil {
		store = litestream.NewStore(nil, litestream.DefaultCompactionLevels)
		store.SnapshotRetention = time.Duration(schedule.Keep) * day
		err = store.Open(ctx)
		b.stores[schedule.Keep] = store
	}
	b.mu.Unlock()
	db := litestream.NewDB(live)
	db.Replica = litestream.NewReplicaWithClient(db, replicaClient(bucket, folder))
	if err == nil {
		err = store.RegisterDB(db)
	}
	if err != nil {
		return fmt.Errorf("writing down every change: %w", err)
	}
	matchOwner(live)
	b.mu.Lock()
	b.watched[live] = watched{bucket: *bucket, folder: folder, keep: schedule.Keep, db: db}
	b.mu.Unlock()
	return nil
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
		list, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket.Bucket), Prefix: aws.String(old)})
		if err != nil || old == current || slices.ContainsFunc(list.Contents, func(object types.Object) bool { return object.LastModified.After(before) }) {
			continue
		}
		for _, object := range list.Contents {
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
