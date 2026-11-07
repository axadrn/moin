package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// What moin leaves next to a database is never a database of the app.
func TestBackupFile(t *testing.T) {
	for name, ours := range map[string]bool{
		"app.db": false, "app.db-wal": false, ".moin-copy-123": true, ".moin-check-9": true,
		"app.db.before-20261107T140000Z": true, "app.db.restore": true,
	} {
		if backupFile(name) != ours {
			t.Errorf("backupFile(%q) = %v", name, !ours)
		}
	}
}

// The crash rule of SQLite: the live file missing and the undo there, the
// undo goes back; the undo there and the live file too, the restore had
// switched. A half built restore goes.
func TestSQLiteCrashRule(t *testing.T) {
	const id = "20261107T140000Z"
	for _, test := range []struct {
		name               string
		live, aside, built bool
		switched           bool
	}{
		{name: "before the first rename", live: true, built: true},
		{name: "between the renames", aside: true, built: true},
		{name: "after the switch", live: true, aside: true, switched: true},
	} {
		dir := t.TempDir()
		live := filepath.Join(dir, "app.db")
		put := func(file, body string, on bool) {
			if on {
				os.WriteFile(file, []byte(body), 0o600)
			}
		}
		put(live, "restored", test.live)
		put(live+".before-"+id, "before", test.aside)
		put(live+".restore", "half", test.built)
		e := &sqliteEngine{b: newBackups(newApps(dir, nil), nil), database: protocol.Database{HostPath: live}}
		switched, err := e.Recover(context.Background(), id)
		if err != nil || switched != test.switched {
			t.Fatalf("%s: switched %v, %v", test.name, switched, err)
		}
		body, _ := os.ReadFile(live)
		if want := map[bool]string{true: "restored", false: "before"}[test.live]; string(body) != want {
			t.Errorf("%s: the live file is %q, want %q", test.name, body, want)
		}
		if exists(live + ".restore") {
			t.Errorf("%s: the half built restore stayed", test.name)
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
	if err != nil || folder != "moin/ws/shop/id-1/data-app.db" {
		t.Fatalf("folder %q %v", folder, err)
	}
	if _, _, err := b.where("blog", protocol.Database{Path: "/data/app.db"}); err == nil {
		t.Fatal("an app without an ID has a folder")
	}
}
