package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/axadrn/ops/protocol"
)

// Postgres backups are snapshots: pg_dump in its custom format, taken
// before every deploy, restore and data edit, on demand and every hour, in
// the same folders as the SQLite snapshots:
//
//	<path>/<workspace>/<app>/<app ID>/postgres-<service>/snapshots/<id>.dump
//
// A point in time restore is SQLite's only, for now.
const (
	pgSnapshotExt   = ".dump"
	pgSnapshotEvery = time.Hour
	pgFirstSnapshot = time.Minute
)

// snapshotExt is the file ending of a database's snapshots in the bucket.
func snapshotExt(database protocol.Database) string {
	if database.Postgres() {
		return pgSnapshotExt
	}
	return ".db.gz"
}

// drillKey names a database in the drill results: its file, or for
// Postgres the app and service.
func drillKey(app string, database protocol.Database) string {
	if database.Postgres() {
		return "postgres:" + app + "/" + database.Service
	}
	return database.HostPath
}

// pgCopy dumps a Postgres database into a file next to the app's releases.
// cleanup removes it.
func (b *backups) pgCopy(ctx context.Context, app string, database protocol.Database) (string, func(), error) {
	target, err := b.apps.postgres(ctx, app, database)
	if err != nil {
		return "", nil, err
	}
	file, err := os.CreateTemp(b.apps.appDir(app), snapshotFolderPrefix+"*"+pgSnapshotExt)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.Remove(file.Name()) }
	err = target.dump(ctx, file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("pg_dump: %w", err)
	}
	return file.Name(), cleanup, nil
}

// downloadDump fetches a Postgres snapshot into a file next to the app's
// releases, from the app's bucket or older, a bucket the app left.
func (b *backups) downloadDump(ctx context.Context, older *protocol.BackupStorage, app string, database protocol.Database, id string) (string, error) {
	b.mu.Lock()
	storage := b.storageFor(app)
	if older != nil {
		storage = older
	}
	if storage == nil {
		b.mu.Unlock()
		return "", errors.New("backups are not set up")
	}
	client, bucket := s3Client(storage), storage.Bucket
	prefix, err := b.prefixIn(storage, app, database.Path)
	b.mu.Unlock()
	if err != nil {
		return "", err
	}
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(prefix + "/snapshots/" + id + pgSnapshotExt)})
	var missing *types.NoSuchKey
	if errors.As(err, &missing) {
		return "", fmt.Errorf("there is no snapshot %s of %s", id, database.Path)
	}
	if err != nil {
		return "", fmt.Errorf("snapshot %s: %w", id, err)
	}
	defer object.Body.Close()
	file, err := os.CreateTemp(b.apps.appDir(app), snapshotFolderPrefix+"restore-*"+pgSnapshotExt)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(file, object.Body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return file.Name(), nil
}

// pgRestore puts a Postgres database back from a snapshot. It takes a
// snapshot of the current state first and stops the app's other services
// while pg_restore runs in one transaction: it changes all or nothing.
func (b *backups) pgRestore(ctx context.Context, request protocol.BackupRestore, database protocol.Database) error {
	if request.At != nil {
		if note := b.walgUnsupported(request.App, database); note != "" {
			return errors.New(note)
		}
		return b.pgRestorePoint(ctx, request, database)
	}
	defer b.apps.lock(request.App)()
	file, err := b.downloadDump(ctx, request.Storage, request.App, database, request.Snapshot)
	if err != nil {
		return err
	}
	defer os.Remove(file)
	target, err := b.apps.postgres(ctx, request.App, database)
	if err != nil {
		return err
	}
	_, err = b.snapshotOne(ctx, request.App, database, "before-restore")
	if err != nil {
		return fmt.Errorf("snapshot before the restore: %w", err)
	}
	composeFile := filepath.Join(b.apps.releaseDir(request.App, b.apps.current(request.App)), "compose.json")
	others := otherServices(composeFile, database.Service)
	noLog := func(string, ...any) {}
	if len(others) > 0 {
		err = b.apps.compose(ctx, request.App, composeFile, noLog, append([]string{"stop"}, others...)...)
		if err != nil {
			return fmt.Errorf("stop the app: %w", err)
		}
	}
	restoreErr := target.restoreDump(ctx, file)
	var startErr error
	if len(others) > 0 {
		startErr = b.apps.compose(ctx, request.App, composeFile, noLog, append([]string{"start"}, others...)...)
	}
	if restoreErr != nil {
		return fmt.Errorf("pg_restore, nothing changed: %w", restoreErr)
	}
	if startErr != nil {
		return fmt.Errorf("start the app again: %w", startErr)
	}
	slog.Info("backup: restored", "app", request.App, "database", database.Path, "snapshot", request.Snapshot)
	return nil
}

// otherServices are the services of a compose file but one, sorted.
func otherServices(composeFile, except string) []string {
	var config struct {
		Services map[string]json.RawMessage `json:"services"`
	}
	body, err := os.ReadFile(composeFile)
	if err != nil || json.Unmarshal(body, &config) != nil {
		return nil
	}
	others := []string{}
	for name := range config.Services {
		if name != except {
			others = append(others, name)
		}
	}
	slices.Sort(others)
	return others
}

// pgDatabases are the running Postgres services of every app.
func (b *backups) pgDatabases() map[string][]protocol.Database {
	found := map[string][]protocol.Database{}
	entries, _ := os.ReadDir(b.apps.dir)
	for _, entry := range entries {
		for _, database := range b.apps.runningDatabases(entry.Name()) {
			if database.Postgres() {
				found[entry.Name()] = append(found[entry.Name()], database)
			}
		}
	}
	return found
}

// pgDrills restores the newest snapshot of every Postgres database into a
// throwaway container and checks it, adding the results to reports. waiting
// reports a database with no snapshot yet.
func (b *backups) pgDrills(ctx context.Context, reports map[string][]protocol.DatabaseBackup) (waiting bool) {
	if _, err := b.ready(); err != nil {
		return false
	}
	for app, databases := range b.pgDatabases() {
		for _, database := range databases {
			err := b.pgDrillOne(ctx, app, database)
			b.mu.Lock()
			if errors.Is(err, errNoSnapshot) {
				waiting = true
				delete(b.verified, drillKey(app, database))
			} else {
				result := drill{at: time.Now().UTC()}
				if err != nil {
					result.err = "restore drill failed: " + err.Error()
					slog.Error("backup: restore drill failed", "app", app, "database", database.Path, "error", err)
				}
				b.verified[drillKey(app, database)] = result
			}
			b.mu.Unlock()
			reports[app] = append(reports[app], b.status(ctx, app, database, false))
		}
	}
	return waiting
}

var errNoSnapshot = errors.New("no snapshot yet")

func (b *backups) pgDrillOne(ctx context.Context, app string, database protocol.Database) error {
	snapshots, err := b.snapshots(ctx, app, database.Path)
	if err != nil {
		return err
	}
	if len(snapshots) == 0 {
		return errNoSnapshot
	}
	target, err := b.apps.postgres(ctx, app, database)
	if err != nil {
		return err
	}
	file, err := b.downloadDump(ctx, nil, app, database, snapshots[0].ID)
	if err != nil {
		return err
	}
	defer os.Remove(file)
	return b.apps.pgDrill(ctx, target.image, file)
}

// pgSnapshots takes a snapshot of every Postgres database once an hour,
// unless it has one from the last hour already, such as from a deploy.
func (b *backups) pgSnapshots(ctx context.Context) {
	timer := time.NewTimer(pgFirstSnapshot)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		b.pgSnapshotAll(ctx)
		timer.Reset(pgSnapshotEvery)
	}
}

func (b *backups) pgSnapshotAll(ctx context.Context) {
	if _, err := b.ready(); err != nil {
		return
	}
	for app, databases := range b.pgDatabases() {
		for _, database := range databases {
			snapshots, err := b.snapshots(ctx, app, database.Path)
			if err == nil && len(snapshots) > 0 && time.Since(snapshots[0].CreatedAt) < pgSnapshotEvery-5*time.Minute {
				continue
			}
			_, err = b.snapshotOne(ctx, app, database, "hourly")
			switch {
			case errors.Is(err, errPostgresDown):
				// A database that does not run has nothing new to copy.
			case err != nil:
				slog.Error("backup: hourly snapshot failed", "app", app, "database", database.Path, "error", err)
				b.fail(drillKey(app, database), "snapshot", "hourly snapshot: "+err.Error())
			default:
				b.fail(drillKey(app, database), "snapshot", "")
			}
		}
	}
}
