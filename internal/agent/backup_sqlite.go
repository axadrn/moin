package agent

import (
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/benbjohnson/litestream"
	lss3 "github.com/benbjohnson/litestream/s3"

	"github.com/axadrn/moin/protocol"
)

// sqliteEngine backs up a SQLite file. A copy is VACUUM INTO, SQLite's
// own clean copy while the app writes, into a temp file next to the
// database, gzipped into the bucket, the temp file deleted at once. With
// Every change, Litestream, inside the agent, also writes each change
// into the bucket, so a restore can go back to any minute.
type sqliteEngine struct {
	b        *backups
	app      string
	database protocol.Database
}

func (e *sqliteEngine) Copy(ctx context.Context, w io.Writer) error {
	dir, err := os.MkdirTemp(filepath.Dir(e.database.HostPath), "."+tempName("copy-*"))
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

// Restore builds <db>.restore and checks it, or takes the undo
// <db>.before-<undo>. With the app stopped it folds the live file's log
// into it, renames it to <db>.before-<id> and the built file in its place.
func (e *sqliteEngine) Restore(ctx context.Context, from source, id string) error {
	live := e.database.HostPath
	aside, built := live+".before-"+id, live+".restore"
	if from.undo != "" {
		built = live + ".before-" + from.undo
		if !exists(built) {
			return fmt.Errorf("there is no state before the restore %s: it is older than the app keeps", from.undo)
		}
	} else {
		removeDatabase(built)
		defer removeDatabase(built)
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
	if e.b.crash != nil && e.b.crash("built") {
		return errCrash
	}
	if err := os.Rename(live, aside); err != nil {
		return errors.Join(fmt.Errorf("switch: %w; nothing changed", err), start())
	}
	if e.b.crash != nil && e.b.crash("aside") {
		return errCrash
	}
	if err := os.Rename(built, live); err != nil {
		return errors.Join(fmt.Errorf("switch: %w", err), os.Rename(aside, live), start())
	}
	_ = syncFile(filepath.Dir(live))
	if e.b.crash != nil && e.b.crash("switched") {
		return errCrash
	}
	// Every change starts again from the restored file.
	_ = os.RemoveAll(litestreamState(live))
	if err := start(); err != nil {
		return fmt.Errorf("the restored data is in place, but %w", err)
	}
	return nil
}

// build writes the database of a copy, or of a minute, into target.
func (e *sqliteEngine) build(ctx context.Context, from source, target string) error {
	if from.copy == nil {
		bucket, folder, err := e.b.where(e.app, e.database)
		if err != nil {
			return err
		}
		options := litestream.NewRestoreOptions()
		options.OutputPath, options.Timestamp = target, from.at
		err = litestream.NewReplicaWithClient(nil, replicaClient(bucket, folder)).Restore(ctx, options)
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

// Recover: the live file missing and the undo there, the undo goes back.
func (e *sqliteEngine) Recover(ctx context.Context, id string) (bool, error) {
	live := e.database.HostPath
	aside := live + ".before-" + id
	removeDatabase(live + ".restore")
	if !exists(live) && exists(aside) {
		if err := os.Rename(aside, live); err != nil {
			return false, err
		}
		_ = syncFile(filepath.Dir(live))
	}
	if !exists(aside) {
		return false, nil
	}
	return true, os.RemoveAll(litestreamState(live))
}

// Check unpacks a copy next to the database and runs SQLite's integrity
// check, which reads every page.
func (e *sqliteEngine) Check(ctx context.Context, copy io.Reader) error {
	dir, err := os.MkdirTemp(filepath.Dir(e.database.HostPath), "."+tempName("check-*"))
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
	files, err := filepath.Glob(e.database.HostPath + ".before-*")
	undos := []protocol.Copy{}
	for index := len(files) - 1; index >= 0; index-- {
		id := strings.TrimPrefix(files[index], e.database.HostPath+".before-")
		at, parseErr := time.Parse(protocol.CopyIDLayout, id)
		if info, statErr := os.Stat(files[index]); parseErr == nil && statErr == nil {
			undos = append(undos, protocol.Copy{ID: id, At: at, Size: info.Size()})
		}
	}
	return undos, err
}

func (e *sqliteEngine) Drop(ctx context.Context, id string) error {
	removeDatabase(e.database.HostPath + ".before-" + id)
	return nil
}

// watched is a file Litestream follows into folder, kept keep days.
type watched struct {
	bucket protocol.Bucket
	folder string
	keep   int
	db     *litestream.DB
}

// record has Litestream follow a file with Every change into the
// litestream/ folder next to its copies. It goes on only where both the
// server and the bucket hold the file's history; else both start fresh,
// as after a restore. Without Every change both go.
func (b *backups) record(ctx context.Context, app string, database protocol.Database, schedule protocol.Schedule) error {
	live := database.HostPath
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return err
	}
	b.mu.Lock()
	old, known := b.watched[live]
	b.mu.Unlock()
	if known && old.bucket == *bucket && old.folder == folder && old.keep == schedule.Keep && schedule.EveryChange {
		if why := old.db.SyncDiagnostic().Error; why != "" {
			return errors.New("writing down every change: " + why)
		}
		return nil
	}
	b.unwatch(live)
	client := replicaClient(bucket, folder)
	list, err := s3Client(bucket).ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket.Bucket), Prefix: aws.String(client.Path + "/"), MaxKeys: aws.Int32(1)})
	if err != nil {
		return err
	}
	if !schedule.EveryChange || exists(litestreamState(live)) != (len(list.Contents) > 0) {
		if err := errors.Join(os.RemoveAll(litestreamState(live)), client.DeleteAll(ctx)); err != nil || !schedule.EveryChange {
			return err
		}
	}
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
	db.Replica = litestream.NewReplicaWithClient(db, client)
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
	bucket, folder, err := b.where(app, database)
	if err != nil {
		return nil, nil
	}
	first, last, err := litestream.NewReplicaWithClient(nil, replicaClient(bucket, folder)).TimeBounds(ctx)
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
	client.Path = folder + "/litestream"
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

// backupFile says a name is one moin leaves next to a database: a copy
// or a check being made, a restore being built, the undo of a restore.
func backupFile(name string) bool {
	return strings.HasPrefix(name, "."+tempName("copy-")) || strings.HasPrefix(name, "."+tempName("check-")) ||
		strings.Contains(name, ".before-") || strings.HasSuffix(name, ".restore")
}

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
