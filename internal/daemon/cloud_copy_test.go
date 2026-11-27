package daemon

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// The address of a copy is https to a public address, nothing else.
func TestCopyAddresses(t *testing.T) {
	for _, bad := range []string{"http://copies.example.com/x", "ftp://copies.example.com/x", "https://user:pw@copies.example.com/x", "https:///x", "not a url"} {
		if err := checkCopyURL(bad); err == nil {
			t.Errorf("%q passed", bad)
		}
	}
	if err := checkCopyURL("https://account.r2.cloudflarestorage.com/bucket/key?X-Amz-Signature=1"); err != nil {
		t.Error(err)
	}
	for _, private := range []string{"127.0.0.1:443", "10.0.0.7:443", "192.168.1.2:443", "100.101.102.103:443", "169.254.169.254:80", "[::1]:443", "[fd00::1]:443", "0.0.0.0:443"} {
		if err := refusePrivate("tcp", private, nil); err == nil {
			t.Errorf("%s was dialed", private)
		}
	}
	if err := refusePrivate("tcp", "203.0.113.7:443", nil); err != nil {
		t.Error(err)
	}
}

// fakeCloud is moin cloud and the bucket it hands out addresses to, over
// https, in memory. taking says whether it takes copies.
type fakeCloud struct {
	mu       sync.Mutex
	taking   bool
	started  []copyStart
	uploaded [][]byte
	signed   []string
	done     int
	watcher  Watcher
}

func newFakeCloud(t *testing.T) *fakeCloud {
	t.Helper()
	cloud := &fakeCloud{taking: true}
	bucket := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloud.mu.Lock()
		defer cloud.mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		cloud.uploaded = append(cloud.uploaded, body)
		cloud.signed = append(cloud.signed, fmt.Sprintf("%d %s", r.ContentLength, r.Header.Get("If-None-Match")))
	}))
	t.Cleanup(bucket.Close)
	before := copyClient
	copyClient = bucket.Client()
	t.Cleanup(func() { copyClient = before })
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloud.mu.Lock()
		defer cloud.mu.Unlock()
		switch {
		case r.Header.Get("Authorization") != "Bearer cloud-token":
			http.Error(w, "unknown token", http.StatusUnauthorized)
		case r.URL.Path == "/api/copies" && !cloud.taking:
			http.Error(w, "the workspace does not pay", http.StatusPaymentRequired)
		case r.URL.Path == "/api/copies":
			var start copyStart
			json.NewDecoder(r.Body).Decode(&start)
			cloud.started = append(cloud.started, start)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(copyUpload{ID: "C1", URL: bucket.URL + "/copies/C1.age"})
		case r.URL.Path == "/api/copies/C1/done":
			cloud.done++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(site.Close)
	cloud.watcher = Watcher{URL: site.URL, Token: "cloud-token"}
	return cloud
}

// opens says body is a copy the server's key opens, with a SQLite file in
// it.
func opens(t *testing.T, body []byte) {
	t.Helper()
	key, err := serverKey()
	if err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptCopy(bytes.NewReader(body), key)
	if err != nil {
		t.Fatal(err)
	}
	unzipped, err := gzip.NewReader(plain)
	if err != nil {
		t.Fatal(err)
	}
	database, err := io.ReadAll(unzipped)
	if err != nil || !bytes.HasPrefix(database, []byte("SQLite format 3")) {
		t.Fatalf("the copy holds %d bytes that are no SQLite file: %v", len(database), err)
	}
}

// noTempLeft says no copy is left in the daemon's own folder.
func noTempLeft(t *testing.T, b *backups) {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(b.apps.tempDir(), "*"))
	if len(left) > 0 {
		t.Errorf("left on the disk: %v", left)
	}
}

// A server with a bucket and no watcher that takes copies has its copy in
// the bucket, and nothing goes to the watcher.
func TestCopyToTheBucketOnly(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	own := httptest.NewServer(http.NotFoundHandler())
	defer own.Close()
	bucket := withFakeBucket(t, b, Config{Watchers: []Watcher{{URL: own.URL, Token: "own-token"}}})
	b.copyIfDue(context.Background(), "shop", database)
	if status := b.status(context.Background(), "shop", database, true); status.Error != "" || len(status.Copies) != 1 {
		t.Fatalf("status: %+v", status)
	}
	for _, body := range bucket.objects {
		opens(t, body)
	}
	noTempLeft(t, b)
}

// A server without a bucket has its copy at moin cloud: the schedule
// counts from it, nothing is called a problem, and no check runs, since
// the daemon can read no copy back.
func TestCopyToCloudOnly(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	cloud := newFakeCloud(t)
	b.configure(Config{Name: "prod", Watchers: []Watcher{cloud.watcher}})
	ctx := context.Background()
	b.copyIfDue(ctx, "shop", database)
	b.checkIfDue(ctx, "shop", database)
	if len(cloud.uploaded) != 1 || cloud.done != 1 {
		t.Fatalf("uploads %d, done %d", len(cloud.uploaded), cloud.done)
	}
	opens(t, cloud.uploaded[0])
	if start := cloud.started[0]; start.App != "shop" || start.Database != "/data/app.db" || start.Size != int64(len(cloud.uploaded[0])) || cloud.signed[0] != fmt.Sprintf("%d *", start.Size) {
		t.Errorf("started %+v, signed %q", start, cloud.signed[0])
	}
	status := b.status(ctx, "shop", database, false)
	if status.Error != "" || status.CopiedAt == nil || status.CheckedAt != nil {
		t.Errorf("status: %+v", status)
	}
	// The next copy waits for its time.
	b.copyIfDue(ctx, "shop", database)
	if len(cloud.uploaded) != 1 {
		t.Errorf("a second copy at once: %d", len(cloud.uploaded))
	}
	noTempLeft(t, b)
}

// With a bucket and moin cloud, both get the same bytes.
func TestCopyToTheBucketAndCloud(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	cloud := newFakeCloud(t)
	bucket := withFakeBucket(t, b, Config{Watchers: []Watcher{cloud.watcher}})
	b.copyIfDue(context.Background(), "shop", database)
	if len(bucket.objects) != 1 || len(cloud.uploaded) != 1 {
		t.Fatalf("the bucket has %d, moin cloud %d", len(bucket.objects), len(cloud.uploaded))
	}
	for _, body := range bucket.objects {
		if !bytes.Equal(body, cloud.uploaded[0]) {
			t.Error("the bucket and moin cloud hold different bytes")
		}
		opens(t, body)
	}
	// moin cloud away: the copy in the bucket counts, nothing is a problem.
	cloud.taking = false
	b.mu.Lock()
	b.state("shop", database).LastCopy = time.Time{}
	b.mu.Unlock()
	bucket.objects = map[string][]byte{}
	b.copyIfDue(context.Background(), "shop", database)
	b.mu.Lock()
	s := *b.state("shop", database)
	b.mu.Unlock()
	if len(bucket.objects) != 1 || s.CopyError != "" || s.LastCopy.IsZero() {
		t.Errorf("the bucket has %d, state %+v", len(bucket.objects), s)
	}
	noTempLeft(t, b)
}

// Without a bucket and with no watcher that takes copies, the server has
// no backup: it says "no bucket yet", alerts nothing, and waits before it
// makes the next copy for nobody.
func TestCopyNobodyTakes(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	cloud := newFakeCloud(t)
	cloud.taking = false
	b.configure(Config{Name: "prod", Watchers: []Watcher{cloud.watcher}})
	ctx := context.Background()
	b.copyIfDue(ctx, "shop", database)
	b.mu.Lock()
	s := *b.state("shop", database)
	b.mu.Unlock()
	if s.CopyError != "" || s.CloudTried.IsZero() || !s.LastCopy.IsZero() {
		t.Fatalf("state: %+v", s)
	}
	if status := b.status(ctx, "shop", database, false); status.Error != errNoBucket.Error() {
		t.Errorf("status: %+v", status)
	}
	// It takes copies now, but the next try waits.
	cloud.taking = true
	b.copyIfDue(ctx, "shop", database)
	if len(cloud.uploaded) != 0 {
		t.Error("a second copy at once")
	}
	noTempLeft(t, b)
}
