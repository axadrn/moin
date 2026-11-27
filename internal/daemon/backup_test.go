package daemon

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// What moin leaves next to a database is never a database of the app;
// a file of the app with "before" in its name still is.
func TestBackupFile(t *testing.T) {
	for name, ours := range map[string]bool{
		"app.db": false, "app.db-wal": false, "data.before-migration.db": false, "app.db.restore": false,
		".moin-copy-123": true, ".moin-check-9": true, "app.db.moin-before-20261107T140000Z": true, "app.db.moin-restore": true,
	} {
		if backupFile(name) != ours {
			t.Errorf("backupFile(%q) = %v", name, !ours)
		}
	}
}

// After a crash the undo tells: a second name of the live file means the
// switch never happened, and it goes; another file means it did. A half
// built restore goes.
func TestSQLiteRecover(t *testing.T) {
	const id = "20261107T140000Z-000001"
	for _, switched := range []bool{false, true} {
		dir := t.TempDir()
		live := filepath.Join(dir, "app.db")
		os.WriteFile(live, []byte("before"), 0o600)
		os.WriteFile(live+".moin-restore", []byte("half"), 0o600)
		os.Link(live, undoFile(live, id))
		if switched {
			os.Rename(live+".moin-restore", live)
		}
		e := &sqliteEngine{b: newBackups(newApps(dir)), database: protocol.Database{HostPath: live}}
		got, err := e.Recover(context.Background(), restoreMark{Undo: id})
		if err != nil || got != switched {
			t.Fatalf("switched %v: Recover says %v, %v", switched, got, err)
		}
		if exists(undoFile(live, id)) != switched || exists(live+".moin-restore") || !exists(live) {
			t.Errorf("switched %v: undo %v, built %v", switched, exists(undoFile(live, id)), exists(live+".moin-restore"))
		}
	}
	// No live file, the undo there: the undo goes back.
	dir := t.TempDir()
	live := filepath.Join(dir, "app.db")
	os.WriteFile(undoFile(live, id), []byte("before"), 0o600)
	e := &sqliteEngine{b: newBackups(newApps(dir)), database: protocol.Database{HostPath: live}}
	if got, err := e.Recover(context.Background(), restoreMark{Undo: id}); err != nil || got {
		t.Fatalf("no live file: %v %v", got, err)
	}
	if body, _ := os.ReadFile(live); string(body) != "before" {
		t.Fatalf("the live file is %q", body)
	}
}

// The copies before a change have an ID no scheduled copy has, and two
// in one second wait for the next.
func TestBeforeCopyIDs(t *testing.T) {
	at := time.Date(2026, 11, 7, 14, 0, 0, 0, time.UTC)
	for range 1000 {
		if protocol.NewCopyID(at) == protocol.BeforeCopyID(at) {
			t.Fatal("a scheduled copy has the ID of a copy before a change")
		}
	}
	b := newBackups(newApps(t.TempDir()))
	b.configure(Config{Name: "prod", Bucket: &Bucket{Bucket: "b"}})
	first := b.copyBefore(context.Background(), "shop", "", func(string, ...any) {})
	b.before["shop"] = time.Now().UTC().Truncate(time.Second)
	started := time.Now()
	b.copyBefore(context.Background(), "shop", "", func(string, ...any) {})
	if first != nil || !b.before["shop"].After(started.Truncate(time.Second)) {
		t.Fatalf("two copies before a change in one second: %v", b.before["shop"])
	}
}

// Copy IDs are sorted by time, apart within one second, and the old
// IDs of only the time stay valid.
func TestCopyIDs(t *testing.T) {
	at := time.Date(2026, 11, 7, 14, 0, 0, 0, time.UTC)
	one, two := protocol.NewCopyID(at), protocol.NewCopyID(at)
	if got, ok := protocol.CopyTime(one); !ok || !got.Equal(at) || !protocol.ValidCopyID("20261107T140000Z") || protocol.ValidCopyID("../x") {
		t.Fatalf("%s: %v %v", one, got, ok)
	}
	for one == two {
		two = protocol.NewCopyID(at)
	}
	if protocol.NewCopyID(at.Add(time.Second)) <= one {
		t.Error("IDs are not sorted by time")
	}
}

// A bucket that never answers ends the listing in time.
func TestABucketThatNeverAnswers(t *testing.T) {
	release := make(chan struct{})
	bucket := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer bucket.Close()
	defer close(release)
	a := newApps(t.TempDir())
	os.MkdirAll(a.appDir("shop"), 0o700)
	b := newBackups(a)
	b.configure(Config{Name: "prod", Bucket: &Bucket{Bucket: "b", Endpoint: bucket.URL, AccessKey: "k", SecretKey: "s"}})

	began := time.Now()
	_, err := b.copies(context.Background(), "shop", protocol.Database{Path: "/data/app.db"})
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("err = %v", err)
	}
	if took := time.Since(began); took > listWait+2*time.Second {
		t.Errorf("took %s", took)
	}
}

// Every copy of a database has its place in the bucket, under the
// server's name.
func TestCopyKey(t *testing.T) {
	a := newApps(t.TempDir())
	os.MkdirAll(a.appDir("shop"), 0o700)
	b := newBackups(a)
	if _, _, err := b.where("shop", protocol.Database{Path: "/data/app.db"}); err == nil {
		t.Fatal("a folder without a bucket")
	}
	b.configure(Config{Name: "prod", Bucket: &Bucket{Bucket: "b", Path: "moin"}})
	_, folder, err := b.where("shop", protocol.Database{Path: "/data/app.db"})
	if err != nil || folder != "moin/prod/shop/data~app.db" {
		t.Fatalf("folder %q %v", folder, err)
	}
}

// moin's own Postgres databases fit Postgres's 63 characters for any
// database name and never are the database itself.
func TestMoinNames(t *testing.T) {
	long := strings.Repeat("x", 63)
	for _, name := range []string{moinName("restore", long), moinName("check", long), undoName(long, "20261107T140000Z")} {
		if len(name) > 63 || !strings.HasPrefix(name, "moin_") || name == long {
			t.Errorf("%q", name)
		}
	}
	if moinName("check", "shop") == moinName("check", "shop2") {
		t.Error("two databases share a check")
	}
}

// Two paths that only differ by / and - never share a folder.
func TestDatabaseFolder(t *testing.T) {
	config := Config{Name: "prod", Bucket: &Bucket{}}
	if databaseKey(config, "a", "/data/app.db") == databaseKey(config, "a", "/data-app.db") ||
		databaseKey(config, "a", "postgres-db/analytics") == databaseKey(config, "a", "postgres-db-analytics") {
		t.Error("two databases share a folder")
	}
}

// A disk without room for one more database says so, in numbers.
func TestRoom(t *testing.T) {
	if err := room(t.TempDir(), 1<<60); err == nil || !strings.Contains(err.Error(), "free some space") {
		t.Fatalf("room: %v", err)
	}
	if err := room(t.TempDir(), 0); err != nil {
		t.Fatal(err)
	}
}

// Copies that stopped coming are an error, and a stray
// temp copy of a stopped daemon goes at the next start.
func TestLateAndSweep(t *testing.T) {
	dir := t.TempDir()
	b := newBackups(newApps(dir))
	b.configure(Config{Name: "prod", Bucket: &Bucket{Bucket: "b"}})
	database := protocol.Database{Path: "/data/app.db", HostPath: filepath.Join(dir, "app.db")}
	b.mu.Lock()
	b.state("shop", database).LastCopy = time.Now().Add(-3 * time.Hour)
	b.mu.Unlock()
	if status := b.status(context.Background(), "shop", database, false); !strings.Contains(status.Error, "the copies are late") {
		t.Fatalf("late: %+v", status)
	}
	os.MkdirAll(filepath.Join(dir, ".moin-copy-123"), 0o700)
	root, name, err := databaseFolder(database)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	sweepTemp(root, name)
	if exists(filepath.Join(dir, ".moin-copy-123")) {
		t.Fatal("the stray copy stayed")
	}
}

// A copy before a deploy that fails says so in the deploy log.
func TestCopyBeforeSaysWhenItFails(t *testing.T) {
	a := newApps(t.TempDir())
	file, _ := sqliteDatabase(t)
	release := a.releaseDir("shop", 1)
	os.MkdirAll(release, 0o700)
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Databases: []protocol.Database{{Path: "/data/app.db", HostPath: file}}})
	os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600)
	b := newBackups(a)
	b.configure(Config{Name: "prod", Bucket: &Bucket{Endpoint: "http://127.0.0.1:1", Bucket: "b", AccessKey: "x", SecretKey: "y"}})
	var logged []string
	if at := b.copyBefore(context.Background(), "shop", "", func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }); at != nil || len(logged) != 1 || !strings.Contains(logged[0], "before the change failed") {
		t.Fatalf("copy before: %v %v", at, logged)
	}
}

func sqliteDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", "file:"+file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY)`)
	return file, db
}

// An edit takes the app's lock, as a deploy and a restore do: it waits
// for one that holds it.
func TestEditsTakeTheAppLock(t *testing.T) {
	a := newApps(t.TempDir())
	file, _ := sqliteDatabase(t)
	release := a.releaseDir("shop", 1)
	os.MkdirAll(release, 0o700)
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Databases: []protocol.Database{{Path: "/data/app.db", HostPath: file}}})
	os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600)
	a.backups = newBackups(a)
	unlock := a.lock("shop")
	done := make(chan error, 1)
	go func() {
		_, err := a.query(context.Background(), protocol.DBQuery{DBRequest: protocol.DBRequest{App: "shop"}, SQL: `INSERT INTO notes DEFAULT VALUES`, Write: true})
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("the edit did not wait for the lock")
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// A restore that waits for the app's lock already keeps edits out: none
// slips in between.
func TestEditsRefusedWhileARestoreWaits(t *testing.T) {
	a := newApps(t.TempDir())
	file, _ := sqliteDatabase(t)
	release := a.releaseDir("shop", 1)
	os.MkdirAll(release, 0o700)
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Databases: []protocol.Database{{Path: "/data/app.db", HostPath: file}}})
	os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600)
	a.backups = newBackups(a)
	unlock := a.lock("shop")
	done := make(chan error, 1)
	go func() {
		done <- a.backups.restore(context.Background(), protocol.BackupRestore{ID: "r1", App: "shop", Copy: "20261107T140000Z"})
	}()
	for !a.backups.busy("shop") {
		time.Sleep(time.Millisecond)
	}
	if _, err := a.query(context.Background(), protocol.DBQuery{DBRequest: protocol.DBRequest{App: "shop"}, SQL: `INSERT INTO notes DEFAULT VALUES`, Write: true}); err == nil || !strings.Contains(err.Error(), "a restore of") {
		t.Fatalf("an edit while a restore waits: %v", err)
	}
	unlock()
	<-done
}

// A copy goes on as long as data moves, and stops when none comes.
func TestStopWhenStuck(t *testing.T) {
	ctx, stop := context.WithCancelCause(context.Background())
	defer stop(nil)
	reader, writer := io.Pipe()
	body, stopWatching := stopWhenStuck(reader, 100*time.Millisecond, 100*time.Millisecond, stop)
	defer stopWatching()
	go func() {
		for range 6 {
			writer.Write([]byte("x"))
			time.Sleep(40 * time.Millisecond)
		}
	}()
	buffer := make([]byte, 1)
	for range 6 {
		if _, err := body.Read(buffer); err != nil {
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			t.Fatal("stopped while data was moving")
		}
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("no data came and nothing stopped")
	}
	if !strings.Contains(context.Cause(ctx).Error(), "no data moved") {
		t.Fatalf("the reason is %v", context.Cause(ctx))
	}
}

// A big database gets longer to send its first byte: SQLite writes its
// clean copy first.
func TestFirstDataLimit(t *testing.T) {
	if got := firstDataLimit(0); got != stallLimit {
		t.Errorf("empty: %s", got)
	}
	if got := firstDataLimit(100 << 30); got < stallLimit+2*time.Hour {
		t.Errorf("100 GB: %s", got)
	}
}
