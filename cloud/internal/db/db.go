// Package db opens the SQLite file of moin cloud and brings its tables up
// to date.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// migrations are the SQL files that make the tables, in the order of
// their numbers. A file is never changed once it ran somewhere: a change
// is a new file.
//
//go:embed migrations/*.sql
var migrations embed.FS

// Open opens the database at path, makes it when it is not there, and
// runs the migrations it has not run yet.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if err := migrate(database); err != nil {
		database.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return database, nil
}

// migrate runs every migration newer than the database. SQLite's
// user_version holds the number of the last one that ran.
func migrate(database *sql.DB) error {
	var version int
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	for _, name := range names {
		number, err := strconv.Atoi(strings.SplitN(filepath.Base(name), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("%s has no number in front", name)
		}
		if number <= version {
			continue
		}
		text, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		if err := run(database, string(text), number); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// run runs one migration and counts it, all or nothing.
func run(database *sql.DB, text string, number int) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(text); err != nil {
		return err
	}
	if _, err := tx.Exec("PRAGMA user_version = " + strconv.Itoa(number)); err != nil {
		return err
	}
	return tx.Commit()
}
