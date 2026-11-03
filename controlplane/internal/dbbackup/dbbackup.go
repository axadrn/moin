// Package dbbackup keeps the control plane's own SQLite database in its
// bucket with Litestream, the way the agent keeps the databases of apps:
// every change within seconds, and the database comes back by itself when
// a new server starts without one.
package dbbackup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/benbjohnson/litestream"
	lss3 "github.com/benbjohnson/litestream/s3"
)

// Folder is where the backup lives in the bucket, unless Bucket.Folder
// says otherwise.
const Folder = "controlplane/litestream"

// retention is how far back a restore can go.
const retention = 7 * 24 * time.Hour

// Bucket is where the backup goes, the control plane's own S3 bucket.
type Bucket struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	// Folder in the bucket; Folder by default.
	Folder string
}

// File is the path of the SQLite file in a DB_CONNECTION like
// "./data/app.db?_pragma=...". Anything that is no file has none.
func File(connection string) string {
	file, _, _ := strings.Cut(strings.TrimPrefix(connection, "file:"), "?")
	if file == "" || file == ":memory:" || strings.Contains(file, "://") {
		return ""
	}
	return file
}

func (b Bucket) client() *lss3.ReplicaClient {
	client := lss3.NewReplicaClient()
	client.AccessKeyID = b.AccessKey
	client.SecretAccessKey = b.SecretKey
	client.Region = b.Region
	client.Bucket = b.Bucket
	client.Endpoint = b.Endpoint
	client.ForcePathStyle = b.Endpoint != ""
	client.Path = b.Folder
	if client.Path == "" {
		client.Path = Folder
	}
	return client
}

// RestoreIfMissing brings the database back from the bucket when there is
// no file yet. An empty bucket means a fresh start.
func RestoreIfMissing(ctx context.Context, bucket Bucket, file string) error {
	if _, err := os.Stat(file); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	options := litestream.NewRestoreOptions()
	options.OutputPath = file
	err := litestream.NewReplicaWithClient(nil, bucket.client()).Restore(ctx, options)
	if errors.Is(err, litestream.ErrNoSnapshots) || errors.Is(err, litestream.ErrTxNotAvailable) {
		slog.Info("database backup: nothing in the bucket yet, starting fresh")
		return nil
	}
	if err != nil {
		return fmt.Errorf("restore the database from the bucket: %w", err)
	}
	slog.Info("database restored from the bucket", "file", file)
	return nil
}

// Start sends every change of the database to the bucket until stop is
// called.
func Start(ctx context.Context, bucket Bucket, file string) (stop func(), err error) {
	store := litestream.NewStore(nil, litestream.DefaultCompactionLevels)
	store.SnapshotRetention = retention
	store.Logger = quiet()
	if err := store.Open(ctx); err != nil {
		return nil, fmt.Errorf("start the database backup: %w", err)
	}
	db := litestream.NewDB(file)
	db.SetLogger(quiet())
	db.Replica = litestream.NewReplicaWithClient(db, bucket.client())
	if err := store.RegisterDB(db); err != nil {
		_ = store.Close(context.Background())
		return nil, fmt.Errorf("start the database backup: %w", err)
	}
	slog.Info("database backup on", "bucket", bucket.Bucket, "folder", bucket.client().Path)
	return func() { _ = store.Close(context.Background()) }, nil
}

// quiet is the logger Litestream gets: its warnings and errors, not a line
// for every sync.
func quiet() *slog.Logger {
	return slog.New(warnings{slog.Default().Handler()}).With("part", "database backup")
}

// warnings passes on only records of level warn and up.
type warnings struct{ slog.Handler }

func (w warnings) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn && w.Handler.Enabled(ctx, level)
}

func (w warnings) WithAttrs(attrs []slog.Attr) slog.Handler {
	return warnings{w.Handler.WithAttrs(attrs)}
}

func (w warnings) WithGroup(name string) slog.Handler {
	return warnings{w.Handler.WithGroup(name)}
}
