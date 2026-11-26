package app

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/axadrn/moin/hub/internal/config"
	"github.com/axadrn/moin/hub/internal/db"
	"github.com/axadrn/moin/hub/internal/dbbackup"
)

// Preflight proves that this build starts on the data of cfg, as the start
// does, and never writes the database: it copies app.db into a directory of
// its own next to it, runs New on the copy, which migrates it and opens what
// the database keeps, such as the GitHub App with SECRETS_KEY, and removes
// the directory. history.db is not copied: New makes an empty one. A
// database without a file yet, on a first install or before a restore from
// the bucket, has nothing to prove.
func Preflight(cfg *config.Config) error {
	file := dbbackup.File(cfg.DBConnection)
	if file == "" {
		return nil
	}
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// A preflight that was stopped, such as with Ctrl-C, left its copy
	// behind: it holds the whole database, so it goes first.
	old, _ := filepath.Glob(filepath.Join(filepath.Dir(file), "preflight-*"))
	for _, path := range old {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	dir, err := os.MkdirTemp(filepath.Dir(file), "preflight-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	copied := *cfg
	copied.DBConnection, err = db.CopyInto(cfg.DBConnection, dir)
	if err != nil {
		return err
	}
	application, err := New(&copied)
	if err != nil {
		return err
	}
	return application.Close()
}
