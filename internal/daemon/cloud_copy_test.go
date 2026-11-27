package daemon

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
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
// https, in memory. taking says whether it takes copies, asked how often
// a daemon asked that before it made a copy.
type fakeCloud struct {
	mu       sync.Mutex
	taking   bool
	tooSoon  bool
	asked    int
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
			if start.App == "" {
				cloud.asked++
				http.Error(w, "not a copy", http.StatusBadRequest)
				return
			}
			if cloud.tooSoon {
				http.Error(w, "a copy of this database came less than 50 minutes ago", http.StatusTooManyRequests)
				return
			}
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
	status := b.status(context.Background(), "shop", database, true)
	if status.Error != "" || len(status.Copies) != 1 {
		t.Fatalf("status: %+v", status)
	}
	// It was restored here before it went up: it is the proven copy.
	if status.CheckError != "" || status.CheckedCopy != status.Copies[0].ID {
		t.Errorf("proven %q, the copy is %q: %s", status.CheckedCopy, status.Copies[0].ID, status.CheckError)
	}
	for _, body := range bucket.objects {
		opens(t, body)
	}
	noTempLeft(t, b)
}

// A server without a bucket has its copy at moin cloud: the schedule
// counts from it and nothing is called a problem. The daemon can read no
// copy back, so it restored this one before it went up: it is proven.
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
	if status.Error != "" || status.CopiedAt == nil || status.CheckedAt == nil || status.CheckError != "" {
		t.Errorf("status: %+v", status)
	}
	if at, _ := protocol.CopyTime(status.CheckedCopy); !at.Equal(cloud.started[0].MadeAt) {
		t.Errorf("proven %q, the copy was made at %s", status.CheckedCopy, cloud.started[0].MadeAt)
	}
	if needs := backupNeeds(b.config(), map[string][]protocol.DatabaseBackup{"shop": {status}}); len(needs) != 0 {
		t.Errorf("needs: %+v", needs)
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
	if status := b.status(context.Background(), "shop", database, true); status.CheckError != "" || status.CheckedCopy != status.Copies[0].ID {
		t.Errorf("proven %q, the copy is %q: %s", status.CheckedCopy, status.Copies[0].ID, status.CheckError)
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

// With a bucket, a copy no watcher took is no failed backup: the copy is
// in the bucket. It is written down, the status and the report show it,
// and after a day it is an alert. The next copy a watcher takes ends it.
func TestACopyOnlyTheBucketTookIsWrittenDown(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	cloud := newFakeCloud(t)
	cloud.tooSoon = true
	bucket := withFakeBucket(t, b, Config{Watchers: []Watcher{cloud.watcher}})
	ctx := context.Background()
	b.copyIfDue(ctx, "shop", database)
	status := b.status(ctx, "shop", database, false)
	if len(bucket.objects) != 1 || status.Error != "" || !strings.Contains(status.CloudError, "takes no copy right now") || status.CloudFailingSince == nil {
		t.Fatalf("the bucket has %d, status %+v", len(bucket.objects), status)
	}
	if reported := reportDatabase(status); reported.Error != status.CloudError {
		t.Errorf("the report says %q", reported.Error)
	}
	statuses := map[string][]protocol.DatabaseBackup{"shop": {status}}
	key := "cloud-copy/shop/" + database.Path
	if _, ok := newWatch(b.apps).problems(b.config(), nil, statuses)[key]; ok {
		t.Error("an alert at once")
	}
	b.mu.Lock()
	b.state("shop", database).CloudFailingSince = time.Now().Add(-25 * time.Hour)
	b.mu.Unlock()
	statuses["shop"][0] = b.status(ctx, "shop", database, false)
	if problem, ok := newWatch(b.apps).problems(b.config(), nil, statuses)[key]; !ok || problem.Text != status.CloudError {
		t.Errorf("no alert after a day: %+v", problem)
	}
	cloud.tooSoon = false
	b.mu.Lock()
	b.state("shop", database).LastCopy = time.Time{}
	b.mu.Unlock()
	bucket.objects = map[string][]byte{}
	b.copyIfDue(ctx, "shop", database)
	if status := b.status(ctx, "shop", database, false); len(cloud.uploaded) != 1 || status.CloudError != "" || status.CloudFailingSince != nil {
		t.Errorf("after a copy moin cloud took: %+v", status)
	}
	noTempLeft(t, b)
}

// A watcher that answers 429 takes copies, only not this one: that is a
// copy that failed, with what the watcher said, not "takes no copies".
func TestACopyThatCameTooSoonFails(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	cloud := newFakeCloud(t)
	cloud.tooSoon = true
	b.load = func() (Config, error) { return Config{Watchers: []Watcher{cloud.watcher}}, nil }
	b.copyIfDue(context.Background(), "shop", database)
	b.mu.Lock()
	s := *b.state("shop", database)
	b.mu.Unlock()
	if !strings.Contains(s.CopyError, "takes no copy right now: a copy of this database came less than 50 minutes ago") {
		t.Errorf("state: %+v", s)
	}
	noTempLeft(t, b)
}

// Without a bucket and with no watcher that takes copies, the server has
// no backup and makes no copy for nobody: it asks the watcher, says "no
// bucket yet", alerts nothing, and waits before it asks again.
func TestCopyNobodyTakes(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	cloud := newFakeCloud(t)
	cloud.taking = false
	b.configure(Config{Name: "prod", Watchers: []Watcher{cloud.watcher}})
	ctx := context.Background()
	// A copy of a database that is away fails: none is made.
	live := database.HostPath
	os.Rename(live, live+".away")
	b.copyIfDue(ctx, "shop", database)
	os.Rename(live+".away", live)
	b.mu.Lock()
	s := *b.state("shop", database)
	b.mu.Unlock()
	if s.CopyError != "" || s.CloudTried.IsZero() || !s.LastCopy.IsZero() || !s.CheckedAt.IsZero() {
		t.Fatalf("state: %+v", s)
	}
	status := b.status(ctx, "shop", database, false)
	if status.Error != errNoBucket.Error() {
		t.Errorf("status: %+v", status)
	}
	if needs := backupNeeds(b.config(), map[string][]protocol.DatabaseBackup{"shop": {status}}); len(needs) != 1 || needs[0].Text != "shop: no backup yet." {
		t.Errorf("needs: %+v", needs)
	}
	// It takes copies now, but the next try waits.
	cloud.taking = true
	b.copyIfDue(ctx, "shop", database)
	if len(cloud.started) != 0 || len(cloud.uploaded) != 0 {
		t.Error("a second try at once")
	}
	noTempLeft(t, b)
}

// A server with no bucket and no watcher at all does nothing in its round.
func TestNoCopyWithoutBucketAndWatcher(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	b.configure(Config{Name: "prod"})
	b.tick(context.Background())
	b.mu.Lock()
	s := *b.state("shop", database)
	b.mu.Unlock()
	if s != (state{}) {
		t.Fatalf("state: %+v", s)
	}
	noTempLeft(t, b)
}

// A copy that does not restore goes nowhere, not into the bucket and not
// to moin cloud, and is the problem every surface alerts. The next copy
// waits, is restored first again, and proves the backup.
// A check that has no folder to unpack in could not run: that is not a
// copy that did not restore.
func TestACheckThatCouldNotRunIsNotABrokenCopy(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	if err := os.WriteFile(b.apps.tempDir(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := b.engine("shop", database).Check(context.Background(), source{})
	if !errors.As(err, new(notChecked)) {
		t.Fatalf("the check said: %v", err)
	}
	b.noteCheck("shop", database, "01COPY", false, err)
	status := b.status(context.Background(), "shop", database, false)
	if !strings.HasPrefix(status.CheckError, "could not check the copy 01COPY: ") || status.CheckedCopy != "" {
		t.Errorf("status: %+v", status)
	}
}

func TestACopyThatDoesNotRestoreIsNotUploaded(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	cloud := newFakeCloud(t)
	bucket := withFakeBucket(t, b, Config{Watchers: []Watcher{cloud.watcher}})
	ctx := context.Background()
	// A key that does not open the copy: it cannot be restored.
	b.key, _ = age.GenerateX25519Identity()
	b.copyIfDue(ctx, "shop", database)
	if len(bucket.objects) != 0 || len(cloud.started) != 0 || len(cloud.uploaded) != 0 {
		t.Fatalf("a copy that did not restore: the bucket has %d, moin cloud %d", len(bucket.objects), len(cloud.uploaded))
	}
	status := b.status(ctx, "shop", database, false)
	if !strings.Contains(status.CheckError, " did not restore: ") || status.CheckedCopy != "" || status.CopiedAt != nil || status.Error != "" {
		t.Fatalf("status: %+v", status)
	}
	needs := backupNeeds(b.config(), map[string][]protocol.DatabaseBackup{"shop": {status}})
	if len(needs) != 1 || needs[0].Text != "shop: today's backup is not proven. Last proven copy: none yet." {
		t.Errorf("needs: %+v", needs)
	}
	problems := newWatch(b.apps).problems(b.config(), nil, map[string][]protocol.DatabaseBackup{"shop": {status}})
	if _, ok := problems["check/shop/"+database.Path]; !ok {
		t.Errorf("no alert: %+v", problems)
	}
	// The next copy waits an hour.
	b.key = nil
	b.copyIfDue(ctx, "shop", database)
	if len(bucket.objects) != 0 {
		t.Fatal("the next copy at once")
	}
	b.mu.Lock()
	b.state("shop", database).CheckedAt = time.Now().Add(-2 * time.Hour)
	b.mu.Unlock()
	b.copyIfDue(ctx, "shop", database)
	status = b.status(ctx, "shop", database, true)
	if len(status.Copies) != 1 || len(cloud.uploaded) != 1 || status.CheckError != "" || status.CheckedCopy != status.Copies[0].ID {
		t.Fatalf("the copy after: %+v", status)
	}
	// One proven copy a day: the next one today goes up as it is.
	b.key, _ = age.GenerateX25519Identity()
	b.mu.Lock()
	b.state("shop", database).LastCopy = time.Time{}
	b.mu.Unlock()
	bucket.objects = map[string][]byte{}
	b.copyIfDue(ctx, "shop", database)
	if after := b.status(ctx, "shop", database, false); len(bucket.objects) != 1 || after.CheckError != "" || after.CheckedCopy != status.CheckedCopy {
		t.Errorf("the second copy of the day: %d in the bucket, %+v", len(bucket.objects), after)
	}
	noTempLeft(t, b)
}
