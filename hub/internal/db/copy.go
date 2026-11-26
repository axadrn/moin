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

	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
)

// copiesKept is how many copies from before migrations stay in the
// data directory; older ones go.
const copiesKept = 3

// copyPrefix starts the name of every copy.
const copyPrefix = "backup-before-"

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

// CopyBeforeMigrations copies the database into dir as
// backup-before-<version>.db when this build brings migrations it has not
// run yet, and keeps only the newest few of these copies. It returns the
// path of the copy, or "" when nothing was pending.
func CopyBeforeMigrations(database *sql.DB, dir, version string) (string, error) {
	pending, err := Pending(database)
	if err != nil || !pending {
		return "", err
	}
	name := unsafeName.ReplaceAllString(version, "_")
	if name == "" {
		name = "dev"
	}
	path := filepath.Join(dir, copyPrefix+name+".db")
	// VACUUM INTO refuses a file that exists, such as one of a dev build.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if _, err := database.Exec(`VACUUM INTO ?`, path); err != nil {
		return "", fmt.Errorf("copy the database before migrations: %w", err)
	}
	slog.Info("database copied before migrations", "path", path)
	pruneCopies(dir)
	return path, nil
}

// pruneCopies removes all but the newest copies.
func pruneCopies(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type copied struct {
		path    string
		modTime int64
	}
	var copies []copied
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), copyPrefix) || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		copies = append(copies, copied{filepath.Join(dir, entry.Name()), info.ModTime().UnixNano()})
	}
	slices.SortFunc(copies, func(a, b copied) int { return int(b.modTime - a.modTime) })
	for _, old := range copies[min(len(copies), copiesKept):] {
		if err := os.Remove(old.path); err != nil {
			slog.Warn("remove an old copy of the database", "path", old.path, "error", err)
		}
	}
}

// Check proves that this build starts on the databases of connection
// without touching them: it copies the database and its history.db with
// VACUUM INTO, which works while the running hub writes, migrates each
// copy as the start does and deletes it. A database without a file yet,
// on a first start or before a restore from the bucket, has nothing to
// check.
func Check(connection string) error {
	if err := checkCopy(connection, func(copied *sqlx.DB) error { return RunMigrations(copied.DB) }); err != nil {
		return err
	}
	return checkCopy(HistoryConnection(connection), CreateHistory)
}

// checkCopy copies the database of connection to check-<name> next to it,
// opens the copy with the same settings, runs migrate on it and removes it.
func checkCopy(connection string, migrate func(*sqlx.DB) error) error {
	file, settings, _ := strings.Cut(strings.TrimPrefix(connection, "file:"), "?")
	if file == "" || file == ":memory:" || strings.Contains(file, "://") {
		return nil
	}
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	path := filepath.Join(filepath.Dir(file), "check-"+filepath.Base(file))
	removeCopy := func() {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(path + suffix)
		}
	}
	// VACUUM INTO refuses a file that exists, such as one of a check that
	// was killed.
	removeCopy()
	defer removeCopy()
	source, err := sqlx.Open(databaseDriver, "file:"+file+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	_, err = source.Exec(`VACUUM INTO ?`, path)
	_ = source.Close()
	if err != nil {
		return fmt.Errorf("copy %s: %w", file, err)
	}
	copyConnection := path
	if settings != "" {
		copyConnection += "?" + settings
	}
	copied, err := Init(copyConnection)
	if err != nil {
		return err
	}
	defer copied.Close()
	if err := migrate(copied); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	return copied.Close()
}
