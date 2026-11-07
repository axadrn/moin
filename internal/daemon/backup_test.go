package daemon

import (
	"context"
	"database/sql"
	"fmt"
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

// The crash rule of SQLite, by the step the mark reached: before the
// switch nothing moved; between the renames the undo goes back, and a
// file the app made in the live place meanwhile goes aside as an undo of
// its own; after the switch the restore is done. A half built restore
// goes.
func TestSQLiteCrashRule(t *testing.T) {
	const id = "20261107T140000Z"
	for _, test := range []struct {
		name, step         string
		live, aside, built string
		want               string
		switched, stray    bool
	}{
		{name: "building", step: "", live: "before", built: "half", want: "before"},
		{name: "switching, nothing moved", step: "switching", live: "before", built: "half", want: "before"},
		{name: "between the renames", step: "aside", aside: "before", built: "restored", want: "before"},
		{name: "between the renames, the app made an empty file", step: "aside", live: "empty", aside: "before", built: "restored", want: "before", stray: true},
		{name: "the second rename done", step: "aside", live: "restored", aside: "before", want: "restored", switched: true},
		{name: "switched", step: "switched", live: "restored", aside: "before", want: "restored", switched: true},
	} {
		dir := t.TempDir()
		live := filepath.Join(dir, "app.db")
		for file, body := range map[string]string{live: test.live, undoFile(live, id): test.aside, live + ".moin-restore": test.built} {
			if body != "" {
				os.WriteFile(file, []byte(body), 0o600)
			}
		}
		e := &sqliteEngine{b: newBackups(newApps(dir, nil), nil), database: protocol.Database{HostPath: live}}
		switched, err := e.Recover(context.Background(), restoreMark{Undo: id, Step: test.step})
		if err != nil || switched != test.switched {
			t.Fatalf("%s: switched %v, %v", test.name, switched, err)
		}
		if body, _ := os.ReadFile(live); string(body) != test.want {
			t.Errorf("%s: the live file is %q, want %q", test.name, body, test.want)
		}
		undos, _ := e.Undos(context.Background())
		if exists(live+".moin-restore") || test.stray != (len(undos) == 1 && !switched && test.step == "aside") {
			t.Errorf("%s: left %v, undos %+v", test.name, exists(live+".moin-restore"), undos)
		}
	}
}

// An app without settings, or with broken ones, is copied hourly and
// keeps its copies a week, with every change.
func TestScheduleDefaults(t *testing.T) {
	b := newBackups(newApps(t.TempDir(), nil), nil)
	b.configure(nil, nil, map[string]protocol.Schedule{"shop": {Every: 15, Keep: 3}, "broken": {}})
	if got := b.schedule("shop"); got != (protocol.Schedule{Every: 15, Keep: 3}) {
		t.Errorf("shop: %+v", got)
	}
	for _, app := range []string{"broken", "other"} {
		if got := b.schedule(app); got != protocol.DefaultSchedule {
			t.Errorf("%s: %+v", app, got)
		}
	}
}

// Every copy of a database has its place in the bucket, which the hub
// finds the same way for a download.
func TestCopyKey(t *testing.T) {
	a := newApps(t.TempDir(), nil)
	os.MkdirAll(a.appDir("shop"), 0o700)
	os.WriteFile(filepath.Join(a.appDir("shop"), "app-id"), []byte("id-1"), 0o600)
	b := newBackups(a, nil)
	if _, _, err := b.where("shop", protocol.Database{Path: "/data/app.db"}); err == nil {
		t.Fatal("a folder without a bucket")
	}
	b.configure(&protocol.Bucket{Bucket: "b", Path: "moin", Workspace: "ws"}, nil, nil)
	_, folder, err := b.where("shop", protocol.Database{Path: "/data/app.db"})
	if err != nil || folder != "moin/ws/shop/id-1/data~app.db" {
		t.Fatalf("folder %q %v", folder, err)
	}
	if _, _, err := b.where("blog", protocol.Database{Path: "/data/app.db"}); err == nil {
		t.Fatal("an app without an ID has a folder")
	}
}

// The crash rule the same for every engine, by the step the mark reached.
func TestRecoverFrom(t *testing.T) {
	for _, test := range []struct {
		step                  string
		aside, built          bool
		switched, putBackRuns bool
	}{
		{"", true, true, false, false},
		{"switching", false, true, false, false},
		{"switching", true, true, false, true},
		{"aside", true, true, false, true},
		{"aside", true, false, true, false},
		{"switched", true, false, true, false},
	} {
		ran := false
		switched, err := recoverFrom(test.step, test.aside, test.built, func() error { ran = true; return nil })
		if err != nil || switched != test.switched || ran != test.putBackRuns {
			t.Errorf("%+v: switched %v, put back %v, %v", test, switched, ran, err)
		}
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
	bucket := protocol.Bucket{Workspace: "ws"}
	if protocol.DatabaseFolder(bucket, "a", "1", "/data/app.db") == protocol.DatabaseFolder(bucket, "a", "1", "/data-app.db") ||
		protocol.DatabaseFolder(bucket, "a", "1", "postgres-db/analytics") == protocol.DatabaseFolder(bucket, "a", "1", "postgres-db-analytics") {
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

// Copies that stopped coming are an error the hub hears, and a stray
// temp copy of a stopped daemon goes at the next start.
func TestLateAndSweep(t *testing.T) {
	dir := t.TempDir()
	b := newBackups(newApps(dir, nil), nil)
	database := protocol.Database{Path: "/data/app.db", HostPath: filepath.Join(dir, "app.db")}
	b.mu.Lock()
	b.state("shop", database).copied = time.Now().Add(-3 * time.Hour)
	b.mu.Unlock()
	if status := b.status(context.Background(), "shop", database, false); !strings.Contains(status.Error, "the copies are late") {
		t.Fatalf("late: %+v", status)
	}
	os.MkdirAll(filepath.Join(dir, ".moin-copy-123"), 0o700)
	sweepTemp(database.HostPath)
	if exists(filepath.Join(dir, ".moin-copy-123")) {
		t.Fatal("the stray copy stayed")
	}
}

// A copy before a deploy that fails says so in the deploy log.
func TestCopyBeforeSaysWhenItFails(t *testing.T) {
	a := newApps(t.TempDir(), nil)
	file, _ := sqliteDatabase(t)
	release := a.releaseDir("shop", 1)
	os.MkdirAll(release, 0o700)
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Databases: []protocol.Database{{Path: "/data/app.db", HostPath: file}}})
	os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600)
	os.WriteFile(filepath.Join(a.appDir("shop"), "app-id"), []byte("id-1"), 0o600)
	b := newBackups(a, nil)
	b.configure(&protocol.Bucket{Endpoint: "http://127.0.0.1:1", Bucket: "b", Workspace: "ws", AccessKey: "x", SecretKey: "y"}, nil, nil)
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
