package daemon

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/aws/smithy-go"

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

// Copies that stopped coming are an error.
func TestLate(t *testing.T) {
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
}

// What a copy or a check that was cut off left in the daemon's folder
// goes at the next start, once it is a day old.
func TestRemoveOldTemp(t *testing.T) {
	a := newApps(t.TempDir())
	old, fresh := filepath.Join(a.tempDir(), "moin-copy-1"), filepath.Join(a.tempDir(), "moin-copy-2")
	for _, dir := range []string{old, fresh} {
		os.MkdirAll(dir, 0o700)
		os.WriteFile(filepath.Join(dir, "copy.db"), []byte("x"), 0o600)
	}
	os.Chtimes(old, time.Now().Add(-2*day), time.Now().Add(-2*day))
	// What a deploy of a followed branch left when the daemon stopped
	// goes too; the follow and its key stay.
	clone := filepath.Join(followDir(filepath.Dir(a.dir), "shop"), "deploy-123", "src")
	os.MkdirAll(clone, 0o700)
	saveFollow(filepath.Dir(a.dir), Follow{App: "shop", Repo: "git@example.com:ada/shop.git", Branch: "main"})
	a.removeOldTemp()
	if exists(old) || !exists(fresh) {
		t.Fatalf("old %v, fresh %v", exists(old), exists(fresh))
	}
	if exists(filepath.Dir(clone)) || !exists(filepath.Join(followDir(filepath.Dir(a.dir), "shop"), "follow.json")) {
		t.Fatalf("the clone %v, the follow %v", exists(filepath.Dir(clone)), exists(filepath.Join(followDir(filepath.Dir(a.dir), "shop"), "follow.json")))
	}
}

// Two deploys of one app at once, like one from your laptop and one of a
// branch the server follows, never take the same release number: the
// number is picked under the app's lock, so neither wipes the other's
// release.
func TestTwoDeploysTakeTwoNumbers(t *testing.T) {
	journal(t)
	t.Setenv("MOIN_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	a := newApps(t.TempDir())
	// Both start before either has the lock.
	fromLaptop, fromBranch := a.newStart("shop"), a.newStart("shop")
	if fromLaptop.Number != 0 || fromBranch.Number != 0 {
		t.Fatalf("a number before the lock: %d %d", fromLaptop.Number, fromBranch.Number)
	}
	fromLaptop.SourceFile, fromBranch.SourceFile = filepath.Join(t.TempDir(), "none.tar.gz"), filepath.Join(t.TempDir(), "none.tar.gz")
	unlock := a.lock("shop")
	var wait sync.WaitGroup
	for _, start := range []protocol.DeployStart{fromLaptop, fromBranch} {
		wait.Go(func() {
			// Neither has a source to build: each fails after it made its release.
			if _, err := a.deployHere(context.Background(), start, io.Discard); err == nil {
				t.Error("a deploy without a source worked")
			}
		})
	}
	time.Sleep(100 * time.Millisecond)
	unlock()
	wait.Wait()
	if got := a.releases("shop"); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("releases %v, want 1 and 2", got)
	}
}

// shopWithSQLite is the app shop with one SQLite database, and its
// backups with a server key of their own.
func shopWithSQLite(t *testing.T) (*backups, protocol.Database) {
	t.Helper()
	t.Setenv("MOIN_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	a := newApps(t.TempDir())
	file, _ := sqliteDatabase(t)
	database := protocol.Database{Path: "/data/app.db", HostPath: file}
	release := a.releaseDir("shop", 1)
	os.MkdirAll(release, 0o700)
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Databases: []protocol.Database{database}})
	os.WriteFile(filepath.Join(a.appDir("shop"), "current"), []byte("1"), 0o600)
	return a.backups, database
}

// fakeBucket is a bucket in memory that takes, lists, hands out and
// deletes copies. cut makes that many reads of an object break half way.
type fakeBucket struct {
	mu      sync.Mutex
	objects map[string][]byte
	deleted []string
	cut     int
}

func (f *fakeBucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(r.URL.Path, "/b/")
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><ListBucketResult><Name>b</Name><IsTruncated>false</IsTruncated>`)
		for name, body := range f.objects {
			if strings.HasPrefix(name, r.URL.Query().Get("prefix")) {
				fmt.Fprintf(w, `<Contents><Key>%s</Key><Size>%d</Size></Contents>`, name, len(body))
			}
		}
		fmt.Fprint(w, `</ListBucketResult>`)
	case r.Method == http.MethodGet && f.objects[key] == nil:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`)
	case r.Method == http.MethodGet && f.cut > 0:
		f.cut--
		w.Header().Set("Content-Length", strconv.Itoa(len(f.objects[key])))
		w.Write(f.objects[key][:len(f.objects[key])/2])
		// Half of it is on its way before the line breaks.
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	case r.Method == http.MethodGet:
		w.Write(f.objects[key])
	case r.Method == http.MethodPut:
		f.objects[key], _ = io.ReadAll(r.Body)
	case r.Method == http.MethodDelete:
		f.deleted = append(f.deleted, path.Base(key))
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

// withFakeBucket gives the backups a bucket in memory.
func withFakeBucket(t *testing.T, b *backups, config Config) *fakeBucket {
	t.Helper()
	bucket := &fakeBucket{objects: map[string][]byte{}}
	server := httptest.NewServer(bucket)
	t.Cleanup(server.Close)
	config.Name, config.Bucket = "prod", &Bucket{Bucket: "b", Endpoint: server.URL, AccessKey: "k", SecretKey: "s"}
	b.configure(config)
	return bucket
}

// put puts body into the fake bucket as the copy id of database.
func (f *fakeBucket) put(b *backups, database protocol.Database, id string, body []byte) {
	_, folder, _ := b.where("shop", database)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[*copyKey(folder, protocol.Copy{ID: id}, database)] = body
}

// Retention never deletes the three newest copies, so a copy from a
// clock that jumped ahead takes no real one with it, and never the copy
// the last check restored.
func TestPruneKeepsTheNewestAndTheProven(t *testing.T) {
	b, database := shopWithSQLite(t)
	bucket := withFakeBucket(t, b, Config{Backups: Backups{Schedule: Schedule{Keep: "1d"}}})
	now := time.Now().UTC()
	ages := map[string]time.Duration{"jumped": -30 * day, "hour": time.Hour, "two hours": 2 * time.Hour, "three days": 3 * day, "proven": 4 * day, "five days": 5 * day}
	ids := map[string]string{}
	for name, age := range ages {
		ids[name] = protocol.NewCopyID(now.Add(-age))
		bucket.put(b, database, ids[name], []byte("x"))
	}
	b.mu.Lock()
	b.state("shop", database).CheckedCopy = ids["proven"]
	b.mu.Unlock()
	copies, err := b.copies(context.Background(), "shop", database)
	if err != nil || len(copies) != 6 || copies[0].ID != ids["jumped"] {
		t.Fatalf("copies %+v, %v", copies, err)
	}
	if err := b.prune(context.Background(), "shop", database, copies); err != nil {
		t.Fatal(err)
	}
	slices.Sort(bucket.deleted)
	want := []string{ids["five days"] + ".db.gz.age", ids["three days"] + ".db.gz.age"}
	slices.Sort(want)
	if !slices.Equal(bucket.deleted, want) {
		t.Fatalf("deleted %v, want %v", bucket.deleted, want)
	}
}

// The daily check reads a copy once more when the bucket broke off, says
// "could not read" when it breaks again, and "did not restore" only for a
// copy that is broken.
func TestCheckTellsABrokenReadFromABrokenCopy(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	bucket := withFakeBucket(t, b, Config{})
	var good, broken bytes.Buffer
	if err := b.copyEncrypted(context.Background(), b.engine("shop", database), &good); err != nil {
		t.Fatal(err)
	}
	encrypted, _ := b.encrypt(&broken)
	encrypted.Write(bytes.Repeat([]byte("not a database "), 100))
	encrypted.Close()
	id := protocol.NewCopyID(time.Now().Add(-time.Minute))
	bucket.put(b, database, id, good.Bytes())
	checked := func() state {
		b.check(context.Background(), "shop", database)
		b.mu.Lock()
		defer b.mu.Unlock()
		return *b.state("shop", database)
	}
	bucket.cut = 1
	if s := checked(); s.CheckError != "" || s.CheckedCopy != id || bucket.cut != 0 {
		t.Fatalf("one broken read: %+v", s)
	}
	bucket.cut = 2
	if s := checked(); !strings.HasPrefix(s.CheckError, "could not read the copy "+id) || s.CheckedCopy != id {
		t.Fatalf("two broken reads: %+v", s)
	}
	// A check that failed runs again after an hour, not after a day.
	b.mu.Lock()
	b.state("shop", database).CheckedAt = time.Now().Add(-2 * time.Hour)
	b.mu.Unlock()
	b.checkIfDue(context.Background(), "shop", database)
	if s := checked(); s.CheckError != "" {
		t.Fatalf("the check after the bucket came back: %+v", s)
	}
	newer := protocol.NewCopyID(time.Now())
	bucket.put(b, database, newer, broken.Bytes())
	if s := checked(); !strings.HasPrefix(s.CheckError, "the copy "+newer+" did not restore") || s.CheckedCopy != id {
		t.Fatalf("a broken copy: %+v", s)
	}
}

// A copy that fails at the bucket is the bucket's problem, one that fails
// at the database is the database's, and a deploy stops at either.
func TestCopyFailsAtTheBucketOrTheDatabase(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	b.configure(Config{Name: "prod", Bucket: &Bucket{Endpoint: "http://127.0.0.1:1", Bucket: "b", AccessKey: "x", SecretKey: "y"}})
	b.copyIfDue(context.Background(), "shop", database)
	status := b.status(context.Background(), "shop", database, false)
	b.mu.Lock()
	s := *b.state("shop", database)
	b.mu.Unlock()
	if !s.BucketDown || !strings.Contains(s.CopyError, "the bucket: ") || !strings.Contains(s.CopyError, "connection refused") || strings.Contains(s.CopyError, "operation error") {
		t.Fatalf("a bucket that is away: %+v", s)
	}
	// Ten minutes of grace before it is a problem.
	if status.Error != "" {
		t.Fatalf("a problem at once: %+v", status)
	}
	if _, err := b.copyBefore(context.Background(), "shop"); err == nil || !strings.Contains(err.Error(), "the bucket: ") {
		t.Fatalf("the copy before a deploy: %v", err)
	}
	withFakeBucket(t, b, Config{})
	os.Remove(database.HostPath)
	b.copyIfDue(context.Background(), "shop", database)
	b.mu.Lock()
	s = *b.state("shop", database)
	b.mu.Unlock()
	if s.BucketDown || s.CopyError == "" {
		t.Fatalf("a database that is away: %+v", s)
	}
}

// A copy shares the app's restore lock and a restore takes it alone: no
// copy starts during a restore or after one that was cut off, and a
// restore waits for the copy that runs.
func TestCopiesAndRestoresWaitForEachOther(t *testing.T) {
	b, database := shopWithSQLite(t)
	withFakeBucket(t, b, Config{})
	restoring, _ := tryLock(b.restoreLock("shop"), syscall.LOCK_EX)
	if _, err := b.copyNow(context.Background(), "shop", database); err == nil || !strings.Contains(err.Error(), "a restore of shop runs") {
		t.Fatalf("a copy during a restore: %v", err)
	}
	restoring()
	mark := restoreMark{ID: "r1", App: "shop", Database: database.Path, State: protocol.RestoreRunning}
	writeState(b.markFile(mark.ID), mark)
	if _, err := b.copyNow(context.Background(), "shop", database); err == nil {
		t.Fatal("a copy after a restore that was cut off, before it was tidied")
	}
	os.Remove(b.markFile(mark.ID))

	copying, ok := tryLock(b.restoreLock("shop"), syscall.LOCK_SH)
	alsoCopying, ok2 := tryLock(b.restoreLock("shop"), syscall.LOCK_SH)
	if !ok || !ok2 {
		t.Fatal("two copies do not share the lock")
	}
	alsoCopying()
	var said bytes.Buffer
	locked := make(chan func(), 1)
	go func() {
		unlock, _ := b.lockRestore(context.Background(), "shop", &said)
		locked <- unlock
	}()
	select {
	case <-locked:
		t.Fatal("the restore did not wait for the copy")
	case <-time.After(300 * time.Millisecond):
	}
	copying()
	(<-locked)()
	if !strings.Contains(said.String(), "waiting for a copy") {
		t.Fatalf("the restore said %q", said.String())
	}
	// A restore that gives up waiting ends.
	copying, _ = tryLock(b.restoreLock("shop"), syscall.LOCK_SH)
	defer copying()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := b.lockRestore(ctx, "shop", io.Discard); err == nil {
		t.Fatal("a cancelled restore got the lock")
	}
}

// A restore to a copy or an undo that is not there says so, and writes
// nothing down.
func TestRestoreToWhatIsNotThere(t *testing.T) {
	b, _ := shopWithSQLite(t)
	withFakeBucket(t, b, Config{})
	dir := filepath.Dir(b.apps.dir)
	for to, want := range map[string]string{
		"20200101T000000Z-123456":      "has no copy 20200101T000000Z-123456",
		"undo-20200101T000000Z-123456": "has no undo undo-20200101T000000Z-123456",
		"undo-garbage":                 "is not an undo",
		"yesterday":                    "is not a copy, a time",
	} {
		a := newApps(dir)
		a.backups.load = b.load
		_, err := restoreHere(context.Background(), a, Restore{App: "shop", To: to}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "NoSuchKey") {
			t.Errorf("restore --to %s: %v", to, err)
		}
	}
	if marks := b.marks(); len(marks) != 0 {
		t.Fatalf("written down: %+v", marks)
	}
}

// What a bucket says comes out short.
func TestShortS3(t *testing.T) {
	denied := fmt.Errorf("operation error S3: PutObject, https response error StatusCode: 403, RequestID: X, HostID: Y, %w", &smithy.GenericAPIError{Code: "AccessDenied", Message: "Access Denied"})
	if got := shortS3(denied); got != "AccessDenied: Access Denied" {
		t.Errorf("shortS3 = %q", got)
	}
	away := fmt.Errorf("operation error S3: ListObjectsV2, exceeded maximum number of attempts, 3, %w", &url.Error{Op: "Get", URL: "http://s3.example.com/b", Err: errors.New("dial tcp: connection refused")})
	if got := shortS3(away); got != "dial tcp: connection refused" {
		t.Errorf("shortS3 = %q", got)
	}
	if got := (bucketError{denied}).Error(); got != "the bucket: AccessDenied: Access Denied" {
		t.Errorf("bucketError = %q", got)
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
