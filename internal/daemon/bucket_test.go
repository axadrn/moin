package daemon

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// testRunner is a daemon without a session, whose answers are dropped.
func testRunner(t *testing.T) *runner {
	t.Helper()
	dir := t.TempDir()
	r := &runner{dir: dir, rulesFile: "/etc/moin/rules", pending: map[string]protocol.DeployDone{}}
	r.apps = newApps(dir, r.send)
	r.backups = newBackups(r.apps, r.send)
	r.security = newSecurity(dir, r.apps)
	return r
}

// TestBucketSet: the server keeps a bucket's key only when a test file
// goes in with it, takes the key it keeps when none comes, and gives it to
// every backup config. The rule backups-to refuses another bucket.
func TestBucketSet(t *testing.T) {
	var writes []string
	accept := true
	bucket := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writes = append(writes, r.Method+" "+r.URL.Path)
		if !accept {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer bucket.Close()
	r := testRunner(t)
	set := func(request protocol.BucketSet, rules *protocol.Rules) {
		message, _ := protocol.NewMessage(protocol.TypeBucketSet, request)
		message.RequestID = "r1"
		r.bucketSet(t.Context(), message, rules)
	}
	backups := protocol.Bucket{ID: "b1", Name: "backups", Endpoint: bucket.URL, Region: "auto", Bucket: "moin", Workspace: "ws-1", AccessKey: "key"}

	accept = false
	set(protocol.BucketSet{Bucket: backups, SecretKey: "wrong"}, nil)
	if r.security.bucketKey("b1") != "" {
		t.Fatal("a key the bucket refused was kept")
	}
	accept = true
	set(protocol.BucketSet{Bucket: backups, SecretKey: "right"}, nil)
	if r.security.bucketKey("b1") != "right" || !strings.Contains(strings.Join(writes, " "), "PUT /moin/ws-1/.moin-check") {
		t.Fatalf("key = %q, writes = %q", r.security.bucketKey("b1"), writes)
	}
	// Moved to another path, with the key the server keeps.
	moved := backups
	moved.Path = "elsewhere"
	writes = nil
	set(protocol.BucketSet{Bucket: moved}, nil)
	if len(writes) == 0 || !strings.Contains(writes[0], "/moin/elsewhere/ws-1/") {
		t.Fatalf("writes = %q", writes)
	}
	host, _ := url.Parse(bucket.URL)
	set(protocol.BucketSet{Bucket: protocol.Bucket{ID: "b2", Endpoint: bucket.URL, Bucket: "other", Workspace: "ws-1", AccessKey: "k"}, SecretKey: "s"}, &protocol.Rules{BackupsTo: host.Host + "/moin"})
	if r.security.bucketKey("b2") != "" {
		t.Fatal("backups-to let another bucket in")
	}

	message, _ := protocol.NewMessage(protocol.TypeBackupConfig, protocol.BackupConfig{Bucket: &backups, AppBuckets: map[string]*protocol.Bucket{"shop": {ID: "none", Bucket: "x", Workspace: "ws-1"}}})
	message.RequestID = "r2"
	r.backupConfig(message, nil)
	r.backups.mu.Lock()
	defer r.backups.mu.Unlock()
	if r.backups.bucket.SecretKey != "right" || r.backups.buckets["shop"].SecretKey != "" {
		t.Fatalf("configured %+v and %+v", r.backups.bucket, r.backups.buckets["shop"])
	}
}

// TestBucketNeverTravels: a bucket's secret key is never in a message.
func TestBucketNeverTravels(t *testing.T) {
	message, _ := protocol.NewMessage(protocol.TypeBackupConfig, protocol.BackupConfig{Bucket: &protocol.Bucket{ID: "b1", SecretKey: "s3cret"}})
	if strings.Contains(string(message.Data), "s3cret") {
		t.Fatalf("backup.config carries the key: %s", message.Data)
	}
}

// TestBackupLink: the daemon signs the download link with the key it
// keeps, for the copy's place in the app's bucket.
func TestBackupLink(t *testing.T) {
	r := testRunner(t)
	if err := os.MkdirAll(r.apps.releaseDir("shop", 1), 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(r.apps.appDir("shop"), "current"), []byte("1"), 0o600)
	os.WriteFile(filepath.Join(r.apps.appDir("shop"), "app-id"), []byte("id-1"), 0o600)
	saveJSON(filepath.Join(r.apps.releaseDir("shop", 1), "detected.json"), protocol.Detected{Databases: []protocol.Database{{Path: "/data/app.db"}}})
	r.backups.configure(protocol.BackupConfig{Bucket: &protocol.Bucket{ID: "b1", Endpoint: "https://s3.example.com", Region: "auto", Bucket: "moin", Workspace: "ws-1", AccessKey: "key", SecretKey: "s3cret"}})
	result, err := r.backups.link(t.Context(), protocol.BackupLink{App: "shop", Database: "/data/app.db", Copy: "20261124T120000Z", Name: "my-shop"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"https://s3.example.com/moin/ws-1/shop/id-1/data~app.db/copies/20261124T120000Z.db.gz", "X-Amz-Signature=", "my-shop-data~app.db-20261124T120000Z.db.gz"} {
		if !strings.Contains(result.Link, want) && !strings.Contains(mustUnescape(result.Link), want) {
			t.Errorf("link %s lacks %s", result.Link, want)
		}
	}
	if strings.Contains(result.Link, "s3cret") {
		t.Fatal("the link carries the secret key")
	}
}

func mustUnescape(text string) string {
	unescaped, _ := url.QueryUnescape(text)
	return unescaped
}

// TestBackupLinkObeysDataOff: a download reads data through moin, so
// data: off refuses it and names the line.
func TestBackupLinkObeysDataOff(t *testing.T) {
	r := testRunner(t)
	_, err := r.backupLink(t.Context(), protocol.BackupLink{App: "shop", Database: "/data/app.db", Copy: "c1", Name: "shop"}, &protocol.Rules{DataOff: true})
	if err == nil || err.Error() != "refused by the rules of this server (/etc/moin/rules): data: off" {
		t.Fatalf("err = %v", err)
	}
}
