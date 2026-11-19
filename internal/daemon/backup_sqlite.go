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
// What moin puts next to app.db all starts with app.db.moin-:
// app.db.moin-restore (being built), app.db.moin-before-<id> (an undo).
// Copies and checks are made in the daemon's own folder, which no app can
// reach.
//
// The folder of app.db belongs to the app, which can leave links there.
// So every file the daemon writes, renames or removes next to app.db goes
// through an os.Root of that folder: a link never leads it out, as root.
type sqliteEngine struct {
	b        *backups
	app      string
	database protocol.Database
}

func (e *sqliteEngine) Copy(ctx context.Context, w io.Writer) error {
	if err := e.isFile(); err != nil {
		return err
	}
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

// tempDir is a folder for a copy or a check in the daemon's own folder,
// once the disk has room for need bytes.
func (e *sqliteEngine) tempDir(what string, need int64) (string, error) {
	dir := e.b.apps.tempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := room(dir, need); err != nil {
		return "", err
	}
	return os.MkdirTemp(dir, tempName(what+"-*"))
}

// folder opens the folder the database was found in, and names the
// database in it.
func (e *sqliteEngine) folder() (*os.Root, string, error) { return databaseFolder(e.database) }

// isFile refuses a database that is no longer a plain file in its folder.
func (e *sqliteEngine) isFile() error { return plainDatabase(e.database) }

// databaseFolder opens the folder a SQLite database was found in as an
// os.Root, and names the database in it.
func databaseFolder(database protocol.Database) (*os.Root, string, error) {
	folder := database.HostFolder
	if folder == "" {
		// Found before moin kept the folder.
		folder = filepath.Dir(database.HostPath)
	}
	name, err := filepath.Rel(folder, database.HostPath)
	if err != nil || !within(folder, database.HostPath) {
		return nil, "", fmt.Errorf("%s is not in %s", database.HostPath, folder)
	}
	root, err := os.OpenRoot(folder)
	return root, name, err
}

// plainDatabase refuses a database that is no longer a plain file in its
// folder, such as a link the app put in its place, before SQLite opens it
// by name as root.
func plainDatabase(database protocol.Database) error {
	root, name, err := databaseFolder(database)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a plain file any more, so moin leaves it alone", database.Path)
	}
	return nil
}

// syncIn writes the file or folder name in root to disk.
func syncIn(root *os.Root, name string) error {
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
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
	root, live, err := e.folder()
	if err != nil {
		return err
	}
	defer root.Close()
	aside, built := undoFile(live, id), live+".moin-restore"
	if from.undo != "" {
		built = undoFile(live, from.undo)
		if _, err := root.Lstat(built); err != nil {
			return fmt.Errorf("there is no state before the restore %s: it is older than the app keeps", from.undo)
		}
	} else {
		defer removeDatabase(root, built)
		if err := e.buildChecked(ctx, root, from, built); err != nil {
			return err
		}
	}
	info, err := root.Lstat(live)
	if _, asideErr := root.Lstat(aside); err != nil || !info.Mode().IsRegular() || asideErr == nil {
		return errors.New("the live file is missing, is not a plain file, or an undo has its name; nothing changed")
	}
	if err := errors.Join(root.Chmod(built, info.Mode().Perm()), syncIn(root, built)); err != nil {
		return err
	}
	matchOwnerOf(root, built, info)
	start, err := e.b.apps.stopServices(ctx, e.app)
	if err != nil {
		return err
	}
	// The app is stopped: nothing changes the folder while SQLite folds the
	// log in by name.
	if err := checkpoint(ctx, root, live); err != nil {
		return errors.Join(fmt.Errorf("fold the log into the database: %w; nothing changed", err), start())
	}
	if err := errors.Join(root.Link(live, aside), syncIn(root, filepath.Dir(live))); err != nil {
		root.Remove(aside)
		return errors.Join(fmt.Errorf("keep the undo: %w; nothing changed", err), start())
	}
	if err := root.Rename(built, live); err != nil {
		root.Remove(aside)
		return errors.Join(fmt.Errorf("switch: %w; nothing changed", err), start())
	}
	_ = syncIn(root, filepath.Dir(live))
	if err := start(); err != nil {
		return fmt.Errorf("the restored data is in place, but %w", err)
	}
	return nil
}

// buildChecked builds the restored database into built in root, without
// following a link out of it, and checks it.
func (e *sqliteEngine) buildChecked(ctx context.Context, root *os.Root, from source, built string) error {
	if err := room(root.Name(), from.size); err != nil {
		return fmt.Errorf("%w; nothing changed", err)
	}
	removeDatabase(root, built)
	out, err := root.OpenFile(built, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("%w; nothing changed", err)
	}
	if err := errors.Join(gunzipCopy(from, out), out.Close()); err != nil {
		return fmt.Errorf("%w; nothing changed", err)
	}
	if err := integrityCheck(ctx, filepath.Join(root.Name(), built)); err != nil {
		return fmt.Errorf("what came back failed the integrity check, nothing changed: %w", err)
	}
	return nil
}

func undoFile(live, id string) string { return live + ".moin-before-" + id }

// gunzipCopy writes the gzipped copy of from to out.
func gunzipCopy(from source, out io.Writer) error {
	unzipped, err := gzip.NewReader(from.copy)
	if err != nil {
		return fmt.Errorf("the copy is not gzipped: %w", err)
	}
	_, err = io.Copy(out, unzipped)
	return err
}

// Recover tells from the undo whether the switch happened. It also
// removes what was half built.
func (e *sqliteEngine) Recover(ctx context.Context, mark restoreMark) (bool, error) {
	root, live, err := e.folder()
	if err != nil {
		return false, err
	}
	defer root.Close()
	aside := undoFile(live, mark.Undo)
	if mark.From == "" {
		defer removeDatabase(root, live+".moin-restore")
	}
	asideInfo, err := root.Lstat(aside)
	if err != nil {
		// No undo: the switch never began.
		return false, nil
	}
	liveInfo, err := root.Lstat(live)
	if err != nil {
		// No live file: the undo is the only one, it goes back.
		return false, root.Rename(aside, live)
	}
	if os.SameFile(liveInfo, asideInfo) {
		// A second name of the live file: the switch never happened.
		root.Remove(aside)
		return false, nil
	}
	// Another file: the switch happened.
	return true, nil
}

// Check unpacks a copy in the daemon's own folder and runs SQLite's
// integrity check, which reads every page.
func (e *sqliteEngine) Check(ctx context.Context, from source) error {
	dir, err := e.tempDir("check", from.size)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "check.db")
	out, err := os.Create(file)
	if err != nil {
		return err
	}
	if err := errors.Join(gunzipCopy(from, out), out.Close()); err != nil {
		return err
	}
	return integrityCheck(ctx, file)
}

func (e *sqliteEngine) Undos(ctx context.Context) ([]protocol.Copy, error) {
	root, live, err := e.folder()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	files, err := filepath.Glob(filepath.Join(root.Name(), undoFile(live, "*")))
	undos := []protocol.Copy{}
	for index := len(files) - 1; index >= 0; index-- {
		name, _ := filepath.Rel(root.Name(), files[index])
		id := strings.TrimPrefix(name, undoFile(live, ""))
		at, ok := protocol.CopyTime(id)
		if info, statErr := root.Lstat(name); ok && statErr == nil && info.Mode().IsRegular() {
			undos = append(undos, protocol.Copy{ID: id, At: at, Size: info.Size()})
		}
	}
	return undos, err
}

func (e *sqliteEngine) Drop(ctx context.Context, id string) error {
	root, live, err := e.folder()
	if err != nil {
		return err
	}
	defer root.Close()
	removeDatabase(root, undoFile(live, id))
	return nil
}

// sweepTemp removes what older versions of moin left next to the database
// live in root: copies, checks and the folder they kept for Litestream.
func sweepTemp(root *os.Root, live string) {
	dir := filepath.Dir(live)
	_ = root.RemoveAll(filepath.Join(dir, "."+filepath.Base(live)+"-litestream"))
	for _, what := range []string{"copy", "check"} {
		left, _ := filepath.Glob(filepath.Join(root.Name(), dir, "."+tempName(what+"-*")))
		for _, path := range left {
			if name, err := filepath.Rel(root.Name(), path); err == nil {
				_ = root.RemoveAll(name)
			}
		}
	}
}

// checkpoint folds the log of the SQLite file live in root into it and
// removes the log.
func checkpoint(ctx context.Context, root *os.Root, live string) error {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(root.Name(), live)+"?_pragma=busy_timeout(5000)")
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
			if err = root.Remove(live + suffix); errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		}
	}
	return err
}

// backupFile says a name is one moin leaves next to a database.
func backupFile(name string) bool { return strings.Contains(name, "."+tempName("")) }

// removeDatabase removes the SQLite file name in root and its log files.
func removeDatabase(root *os.Root, name string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = root.Remove(name + suffix)
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
