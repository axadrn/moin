package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pressly/goose/v3"
)

// snapshotsKept is how many snapshots from before migrations stay in the
// data directory; older ones go.
const snapshotsKept = 3

// snapshotPrefix starts the name of every snapshot file.
const snapshotPrefix = "backup-before-"

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// Pending reports whether the database misses migrations of this build. A
// fresh database has nothing worth keeping and reports false.
func Pending(database *sql.DB) (bool, error) {
	if err := setupGoose(); err != nil {
		return false, err
	}
	var tables int
	err := database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'goose_db_version'`).Scan(&tables)
	if err != nil || tables == 0 {
		return false, err
	}
	current, err := goose.GetDBVersion(database)
	if err != nil {
		return false, err
	}
	migrations, err := goose.CollectMigrations(".", 0, goose.MaxVersion)
	if err != nil {
		return false, err
	}
	last, err := migrations.Last()
	if errors.Is(err, goose.ErrNoNextVersion) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return current > 0 && current < last.Version, nil
}

// SnapshotBeforeMigrations copies the database into dir as
// backup-before-<version>.db when this build brings migrations it has not
// run yet, and keeps only the newest few of these copies. It returns the
// path of the copy, or "" when nothing was pending.
func SnapshotBeforeMigrations(database *sql.DB, dir, version string) (string, error) {
	pending, err := Pending(database)
	if err != nil || !pending {
		return "", err
	}
	name := unsafeName.ReplaceAllString(version, "_")
	if name == "" {
		name = "dev"
	}
	path := filepath.Join(dir, snapshotPrefix+name+".db")
	// VACUUM INTO refuses a file that exists, such as one of a dev build.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, err := database.Exec(`VACUUM INTO ?`, path); err != nil {
		return "", fmt.Errorf("snapshot the database before migrations: %w", err)
	}
	slog.Info("database snapshot before migrations", "path", path)
	pruneSnapshots(dir)
	return path, nil
}

// pruneSnapshots removes all but the newest snapshots.
func pruneSnapshots(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type snapshot struct {
		path    string
		modTime int64
	}
	var snapshots []snapshot
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), snapshotPrefix) || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		snapshots = append(snapshots, snapshot{filepath.Join(dir, entry.Name()), info.ModTime().UnixNano()})
	}
	slices.SortFunc(snapshots, func(a, b snapshot) int { return int(b.modTime - a.modTime) })
	for _, old := range snapshots[min(len(snapshots), snapshotsKept):] {
		if err := os.Remove(old.path); err != nil {
			slog.Warn("remove an old database snapshot", "path", old.path, "error", err)
		}
	}
}
