//go:build docker

package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/axadrn/moin/protocol"
)

// A scheduled copy goes to moin cloud, and comes back from there on its
// own: downloaded, opened with your key, restored with an undo. Run like
// backup_docker_test.go.
func TestCloudCopySQLite(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0o777)
	live := filepath.Join(dir, "app.db")
	app := fmt.Sprintf("bk-cloud-%d", time.Now().UnixNano()%1e6)
	compose := map[string]any{"services": map[string]any{"web": map[string]any{"image": "alpine:latest", "command": []string{"sleep", "infinity"}, "init": true}}}
	test := newBackupTest(t, app, compose, protocol.Database{Path: "/data/app.db", HostPath: live})
	exec := func(query string) string {
		t.Helper()
		db, err := sql.Open("sqlite", "file:"+live+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var list string
		if strings.HasPrefix(query, "SELECT") {
			err = db.QueryRow(query).Scan(&list)
		} else {
			_, err = db.Exec(query)
		}
		if err != nil {
			t.Fatal(err)
		}
		return list
	}
	rows := func() string {
		return exec(`SELECT coalesce(group_concat(body, ','), '') FROM (SELECT body FROM notes ORDER BY id)`)
	}
	exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`)
	exec(`INSERT INTO notes (body) VALUES ('a'), ('b')`)

	// moin cloud and its bucket, in one fake, over https.
	var mu sync.Mutex
	var stored []byte
	cloud := httptest.NewUnstartedServer(nil)
	cloud.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "POST /api/copies":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(copyUpload{ID: "C1", URL: "https://" + r.Host + "/bucket/C1"})
		case "PUT /bucket/C1":
			stored, _ = io.ReadAll(r.Body)
		case "POST /api/copies/C1/done":
			w.WriteHeader(http.StatusNoContent)
		case "GET /bucket/C1":
			w.Write(stored)
		default:
			http.NotFound(w, r)
		}
	})
	cloud.StartTLS()
	defer cloud.Close()
	beforeCopy, beforeWatcher := copyClient, watcherClient
	copyClient, watcherClient = cloud.Client(), cloud.Client()
	defer func() { copyClient, watcherClient = beforeCopy, beforeWatcher }()
	test.b.configure(Config{Name: "prod", Bucket: test.bucket, BackupKey: &BackupKey{Public: test.yours.Recipient().String()},
		Watchers: []Watcher{{URL: cloud.URL, Token: "cloud-token"}}, Backups: Backups{Apps: map[string]Schedule{app: {Every: "15m", Keep: "1d"}}}})

	test.b.tick(test.ctx)
	mu.Lock()
	sent := len(stored)
	mu.Unlock()
	if sent == 0 || len(test.copies()) != 1 {
		t.Fatalf("moin cloud got %d bytes, the bucket %d copies", sent, len(test.copies()))
	}
	exec(`INSERT INTO notes (body) VALUES ('c')`)

	restore := CloudRestore{App: app, Database: "/data/app.db", Copy: "C1", URL: cloud.URL + "/bucket/C1"}
	other, _ := age.GenerateX25519Identity()
	restore.Key = other.String()
	if _, err := CloudRestoreHere(context.Background(), filepath.Dir(test.a.dir), restore, io.Discard); err == nil || rows() != "a,b,c" {
		t.Fatalf("another key: %v, rows %s", err, rows())
	}
	restore.Key = test.yours.String()
	restored, err := CloudRestoreHere(context.Background(), filepath.Dir(test.a.dir), restore, io.Discard)
	if err != nil || rows() != "a,b" || !strings.HasPrefix(restored.Undo, undoPrefix) || restored.To != "C1" {
		t.Fatalf("restored %+v, %v, rows %s", restored, err, rows())
	}
	if !test.running() {
		t.Error("the app did not start again")
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(test.a.dir), ".copy-*")); len(leftovers) != 0 {
		t.Errorf("temp files stayed: %v", leftovers)
	}
}
