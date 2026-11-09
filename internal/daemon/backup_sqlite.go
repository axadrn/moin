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
	"strings"
	"syscall"

	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/protocol"
)

// sqliteEngine backs up a SQLite file. A copy is VACUUM INTO, SQLite's
// own clean copy while the app writes, into a temp file next to the
// database, gzipped into the bucket, the temp file deleted at once.
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
	dir, err := e.tempDir("copy", fileSize(e.database.HostPath))
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
// the disk has room for need bytes.
func (e *sqliteEngine) tempDir(what string, need int64) (string, error) {
	dir := filepath.Dir(e.database.HostPath)
	if err := room(dir, need); err != nil {
		return "", err
	}
	return os.MkdirTemp(dir, "."+tempName(what+"-*"))
}

// Size is how many bytes the database file takes.
func (e *sqliteEngine) Size(ctx context.Context) (int64, error) {
	return fileSize(e.database.HostPath), nil
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

// Restore builds app.db.moin-restore and checks it, or takes the undo
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
		defer removeDatabase(built)
		if err := e.buildChecked(ctx, from, built); err != nil {
			return err
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
	if err := start(); err != nil {
		return fmt.Errorf("the restored data is in place, but %w", err)
	}
	return nil
}

// buildChecked builds the restored database into built and checks it.
func (e *sqliteEngine) buildChecked(ctx context.Context, from source, built string) error {
	if err := room(filepath.Dir(e.database.HostPath), from.size); err != nil {
		return fmt.Errorf("%w; nothing changed", err)
	}
	removeDatabase(built)
	if err := e.build(ctx, from, built); err != nil {
		return fmt.Errorf("%w; nothing changed", err)
	}
	if err := integrityCheck(ctx, built); err != nil {
		return fmt.Errorf("what came back failed the integrity check, nothing changed: %w", err)
	}
	return nil
}

func undoFile(live, id string) string { return live + ".moin-before-" + id }

// build unpacks a copy into target.
func (e *sqliteEngine) build(ctx context.Context, from source, target string) error {
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
	return true, nil
}

// Check unpacks a copy next to the database and runs SQLite's integrity
// check, which reads every page.
func (e *sqliteEngine) Check(ctx context.Context, from source) error {
	dir, err := e.tempDir("check", from.size)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "check.db")
	if err := e.build(ctx, from, file); err != nil {
		return err
	}
	return integrityCheck(ctx, file)
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

// sweepTemp removes the copies and checks a stopped daemon left next to
// a database, and the folder older versions of moin kept there for
// Litestream.
func sweepTemp(live string) {
	_ = os.RemoveAll(filepath.Join(filepath.Dir(live), "."+filepath.Base(live)+"-litestream"))
	for _, what := range []string{"copy", "check"} {
		left, _ := filepath.Glob(filepath.Join(filepath.Dir(live), "."+tempName(what+"-*")))
		for _, dir := range left {
			_ = os.RemoveAll(dir)
		}
	}
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
