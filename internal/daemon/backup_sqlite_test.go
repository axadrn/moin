package daemon

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// fakeS3 is a bucket in memory: enough S3 for Litestream and moin's own
// uploads, so these tests run real Litestream without docker.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]fakeObject
}

type fakeObject struct {
	body     []byte
	meta     http.Header
	modified time.Time
}

func newFakeS3() *fakeS3 { return &fakeS3{objects: map[string]fakeObject{}} }

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/moin-test"), "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Has("list-type"):
		f.list(w, r)
	case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
		f.deleteMany(w, r)
	case r.Method == http.MethodPut:
		f.put(w, r, key)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		f.get(w, r, key)
	}
}

// put stores an object; the SDK may send it in aws-chunked framing.
func (f *fakeS3) put(w http.ResponseWriter, r *http.Request, key string) {
	body, _ := io.ReadAll(r.Body)
	if strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") || strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
		body = unchunk(body)
	}
	meta := http.Header{}
	for name, values := range r.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-amz-meta-") {
			meta[name] = values
		}
	}
	f.objects[key] = fakeObject{body: body, meta: meta, modified: time.Now().UTC()}
	sum := md5.Sum(body)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
}

func unchunk(body []byte) []byte {
	var out bytes.Buffer
	reader := bufio.NewReader(bytes.NewReader(body))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return out.Bytes()
		}
		size, _ := strconv.ParseInt(strings.TrimSpace(strings.SplitN(line, ";", 2)[0]), 16, 64)
		if size == 0 {
			return out.Bytes()
		}
		io.CopyN(&out, reader, size)
		reader.ReadString('\n')
	}
}

func (f *fakeS3) get(w http.ResponseWriter, r *http.Request, key string) {
	object, ok := f.objects[key]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `<Error><Code>NoSuchKey</Code></Error>`)
		return
	}
	for name, values := range object.meta {
		w.Header()[name] = values
	}
	w.Header().Set("Last-Modified", object.modified.Format(http.TimeFormat))
	body := object.body
	if spec, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
		from, to, _ := strings.Cut(spec, "-")
		start, _ := strconv.Atoi(from)
		end := len(body) - 1
		if to != "" {
			end, _ = strconv.Atoi(to)
		}
		end = min(end, len(body)-1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method != http.MethodHead {
			w.Write(body[start : end+1])
		}
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	type content struct {
		Key          string
		LastModified string
		ETag         string
		Size         int
	}
	type prefix struct{ Prefix string }
	var result struct {
		XMLName        xml.Name `xml:"ListBucketResult"`
		IsTruncated    bool
		KeyCount       int
		Contents       []content
		CommonPrefixes []prefix
	}
	query := r.URL.Query()
	want, delimiter, after := query.Get("prefix"), query.Get("delimiter"), query.Get("start-after")
	keys := []string{}
	for key := range f.objects {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	seen := map[string]bool{}
	for _, key := range keys {
		rest, ok := strings.CutPrefix(key, want)
		if !ok || key <= after {
			continue
		}
		if head, _, cut := strings.Cut(rest, delimiter); delimiter != "" && cut {
			if !seen[head] {
				seen[head] = true
				result.CommonPrefixes = append(result.CommonPrefixes, prefix{want + head + delimiter})
			}
			continue
		}
		result.Contents = append(result.Contents, content{Key: key, LastModified: f.objects[key].modified.Format("2006-01-02T15:04:05.000Z"), ETag: `"x"`, Size: len(f.objects[key].body)})
	}
	result.KeyCount = len(result.Contents) + len(result.CommonPrefixes)
	body, _ := xml.Marshal(result)
	w.Write(body)
}

func (f *fakeS3) deleteMany(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Object []struct{ Key string }
	}
	body, _ := io.ReadAll(r.Body)
	xml.Unmarshal(body, &request)
	for _, object := range request.Object {
		delete(f.objects, object.Key)
	}
	fmt.Fprint(w, `<DeleteResult></DeleteResult>`)
}

// sqliteBackupTest is an app with one SQLite file, a bucket in memory and
// a docker that does nothing.
type sqliteBackupTest struct {
	t      *testing.T
	ctx    context.Context
	a      *apps
	b      *backups
	s3     *fakeS3
	bucket *protocol.Bucket
	live   string
	db     protocol.Database
	sent   []protocol.Backups
}

func newSQLiteBackupTest(t *testing.T) *sqliteBackupTest {
	t.Helper()
	docker := t.TempDir()
	os.WriteFile(filepath.Join(docker, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	t.Setenv("PATH", docker+":"+os.Getenv("PATH"))
	s3 := newFakeS3()
	server := httptest.NewServer(s3)
	t.Cleanup(server.Close)
	test := &sqliteBackupTest{t: t, ctx: context.Background(), s3: s3}
	test.a = newApps(t.TempDir(), nil)
	test.b = newBackups(test.a, func(kind protocol.MessageType, _ string, data any) {
		if backups, ok := data.(protocol.Backups); ok && kind == protocol.TypeBackupStatus {
			test.sent = append(test.sent, backups)
		}
	})
	test.a.backups = test.b
	test.live = filepath.Join(t.TempDir(), "app.db")
	test.db = protocol.Database{Path: "/data/app.db", HostPath: test.live}
	release := test.a.releaseDir("shop", 1)
	os.MkdirAll(release, 0o700)
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Databases: []protocol.Database{test.db}})
	os.WriteFile(filepath.Join(test.a.appDir("shop"), "current"), []byte("1"), 0o600)
	os.WriteFile(filepath.Join(test.a.appDir("shop"), "app-id"), []byte("id-1"), 0o600)
	test.bucket = &protocol.Bucket{Endpoint: server.URL, Region: "us-east-1", Bucket: "moin-test", Workspace: "ws", AccessKey: "x", SecretKey: "y"}
	test.b.configure(protocol.BackupConfig{Bucket: test.bucket, Schedules: map[string]protocol.Schedule{"shop": {Every: 60, Keep: 1}}})
	t.Cleanup(func() { test.b.configure(protocol.BackupConfig{}) })
	return test
}

// write opens the file for one write and closes it, as an app that opens
// and closes SQLite per request does.
func (test *sqliteBackupTest) write(statement string) {
	test.t.Helper()
	db, err := sql.Open("sqlite", "file:"+test.live+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		test.t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(statement); err != nil {
		test.t.Fatal(err)
	}
}

func (test *sqliteBackupTest) rows() string {
	test.t.Helper()
	db, err := sql.Open("sqlite", "file:"+test.live+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		test.t.Fatal(err)
	}
	defer db.Close()
	var list string
	if err := db.QueryRow(`SELECT coalesce(group_concat(body, ','), '') FROM (SELECT body FROM notes ORDER BY id)`).Scan(&list); err != nil {
		test.t.Fatal(err)
	}
	return list
}

// reached says the newest state in the bucket has a row with this body.
func reached(ctx context.Context, b *backups, app string, database protocol.Database, body string) bool {
	e := &sqliteEngine{b: b, app: app, database: database}
	file, remove, err := e.restoreNewest(ctx)
	if err != nil {
		return false
	}
	defer remove()
	db, err := sql.Open("sqlite", "file:"+file+"?mode=ro")
	if err != nil {
		return false
	}
	defer db.Close()
	var count int
	return db.QueryRow(`SELECT count(*) FROM notes WHERE body = ?`, body).Scan(&count) == nil && count > 0
}

// settle runs rounds until the row with this body is in the bucket.
func (test *sqliteBackupTest) settle(body string) {
	test.t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		test.b.tick(test.ctx)
		if reached(test.ctx, test.b, "shop", test.db, body) {
			return
		}
	}
	test.t.Fatalf("the row %s never reached the bucket", body)
}

func (test *sqliteBackupTest) restore(request protocol.BackupRestore) {
	test.t.Helper()
	request.ID, request.App, request.Database = fmt.Sprintf("r%d", time.Now().UnixNano()), "shop", test.db.Path
	if err := test.b.restore(test.ctx, request); err != nil {
		test.t.Fatalf("restore %+v: %v", request, err)
	}
}

// One SQLite file from following it to every way back: back to a minute,
// the undo, the daily check, the download, and following it again after
// a deploy.
func TestSQLiteBackups(t *testing.T) {
	test := newSQLiteBackupTest(t)
	test.write(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`)
	test.write(`INSERT INTO notes (body) VALUES ('a')`)

	// Following the file: the round has Litestream write every change.
	test.b.tick(test.ctx)
	test.settle("a")
	if status := test.b.status(test.ctx, "shop", test.db, true); status.Error != "" || status.Since == nil || status.CopiedAt != nil || status.Copies != nil {
		t.Fatalf("the status of a file that is followed: %+v", status)
	}

	// Back to a minute.
	test.write(`INSERT INTO notes (body) VALUES ('b')`)
	test.settle("b")
	time.Sleep(1100 * time.Millisecond)
	noted := time.Now().UTC()
	time.Sleep(1100 * time.Millisecond)
	test.write(`INSERT INTO notes (body) VALUES ('c'); DELETE FROM notes WHERE body = 'a'`)
	test.settle("c")
	test.restore(protocol.BackupRestore{At: &noted})
	if got := test.rows(); got != "a,b" {
		t.Fatalf("after the restore to a minute: %q", got)
	}

	// The undo puts the data of before the restore back.
	undos, err := test.b.engine("shop", test.db).Undos(test.ctx)
	if err != nil || len(undos) != 1 {
		t.Fatalf("undos %+v %v", undos, err)
	}
	test.restore(protocol.BackupRestore{Undo: undos[0].ID})
	if got := test.rows(); got != "b,c" {
		t.Fatalf("after the undo: %q", got)
	}

	// A restore starts a new generation; the round follows it.
	before := generation(test.live)
	test.b.tick(test.ctx)
	if generation(test.live) == before {
		t.Fatal("a restore kept the generation")
	}
	test.write(`INSERT INTO notes (body) VALUES ('d')`)
	test.settle("d")

	// The daily check restores the newest state, reads it and leaves
	// nothing behind.
	test.b.checkAll(test.ctx)
	if status := test.b.status(test.ctx, "shop", test.db, false); status.CheckedAt == nil || status.CheckError != "" {
		t.Fatalf("the daily check: %+v", status)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(test.live), ".moin-temp-*")); len(left) != 0 {
		t.Fatalf("the daily check left %v", left)
	}

	// The download: the newest state, gzipped, under downloads/ beside
	// the database's folder.
	copy, err := test.b.download(test.ctx, protocol.BackupRequest{App: "shop", Database: test.db.Path})
	if err != nil {
		t.Fatal(err)
	}
	_, folder, _ := test.b.where("shop", test.db)
	key := protocol.DownloadFile(folder, copy.ID)
	if key != "ws/shop/id-1/downloads/data~app.db-"+copy.ID+".db.gz" {
		t.Fatalf("the download went to %s", key)
	}
	zipped, err := gzip.NewReader(bytes.NewReader(test.s3.objects[key].body))
	if err != nil {
		t.Fatal(err)
	}
	opened := filepath.Join(t.TempDir(), "download.db")
	body, _ := io.ReadAll(zipped)
	os.WriteFile(opened, body, 0o600)
	if err := integrityCheck(test.ctx, opened); err != nil {
		t.Fatalf("the download: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(test.live), ".moin-temp-*")); len(left) != 0 {
		t.Fatalf("the download left %v", left)
	}

	// After a deploy, Litestream starts over: while nothing follows the
	// file, an app that opens and closes it per request leaves no log.
	// The next round follows it again, on the same generation, with the
	// history kept.
	kept := generation(test.live)
	since, _ := test.b.reach(test.ctx, "shop", test.db)
	test.b.letGo("shop")
	test.write(`INSERT INTO notes (body) VALUES ('e')`)
	if exists(test.live + "-wal") {
		t.Fatal("the log is still there")
	}
	test.b.tick(test.ctx)
	test.write(`INSERT INTO notes (body) VALUES ('f')`)
	test.settle("e")
	test.settle("f")
	if status := test.b.status(test.ctx, "shop", test.db, true); status.Error != "" || status.Since == nil {
		t.Fatalf("after following again: %+v", status)
	}
	if generation(test.live) != kept {
		t.Fatal("following again changed the generation")
	}
	if now, _ := test.b.reach(test.ctx, "shop", test.db); now == nil || !now.Equal(*since) {
		t.Fatalf("the history went: %v, was %v", now, since)
	}
	// The daily check fails loudly when the bucket lost what was written.
	client, err := test.b.replica("shop", test.db)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteAll(test.ctx); err != nil {
		t.Fatal(err)
	}
	test.b.checkAll(test.ctx)
	if status := test.b.status(test.ctx, "shop", test.db, false); !strings.Contains(status.CheckError, "the daily check failed") {
		t.Fatalf("a lost bucket passed the check: %+v", status)
	}
}

// A SQLite deploy or edit takes no copy but keeps its moment, and the
// bucket gets no copies/ folder.
func TestSQLiteMomentBefore(t *testing.T) {
	test := newSQLiteBackupTest(t)
	test.write(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`)
	at := test.b.copyBefore(test.ctx, "shop", "", func(string, ...any) {})
	if at == nil {
		t.Fatal("no moment before the change")
	}
	for key := range test.s3.objects {
		if strings.Contains(key, "/copies/") {
			t.Fatalf("a copy of SQLite: %s", key)
		}
	}
	result, err := test.a.query(test.ctx, protocol.DBQuery{DBRequest: protocol.DBRequest{App: "shop", Database: test.db.Path}, SQL: `INSERT INTO notes (body) VALUES ('x')`, Write: true})
	if err != nil || result.Before == nil {
		t.Fatalf("the moment before an edit: %+v %v", result.Before, err)
	}
}

// Old downloads go in the hourly round; newer ones stay, and so does
// everything else.
func TestSweepDownloads(t *testing.T) {
	test := newSQLiteBackupTest(t)
	for key, age := range map[string]time.Duration{
		"ws/shop/id-1/downloads/old.db.gz":      2 * day,
		"ws/shop/id-1/downloads/new.db.gz":      time.Hour,
		"ws/shop/id-1/uploads/old.tgz":          2 * day,
		"ws/shop/id-1/data~app.db/litestream/x": 2 * day,
	} {
		test.s3.objects[key] = fakeObject{body: []byte("x"), meta: http.Header{}, modified: time.Now().Add(-age).UTC()}
	}
	test.b.sweepBucket(test.ctx)
	for key, want := range map[string]bool{
		"ws/shop/id-1/downloads/old.db.gz":      false,
		"ws/shop/id-1/downloads/new.db.gz":      true,
		"ws/shop/id-1/uploads/old.tgz":          false,
		"ws/shop/id-1/data~app.db/litestream/x": true,
	} {
		if _, ok := test.s3.objects[key]; ok != want {
			t.Errorf("%s: there %v", key, ok)
		}
	}
}

// Litestream's own report of trouble is the status of the file, and
// after a deploy the file is followed again and the trouble is gone.
func TestLitestreamTroubleIsAProblem(t *testing.T) {
	test := newSQLiteBackupTest(t)
	test.write(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`)
	test.b.tick(test.ctx)
	test.write(`INSERT INTO notes (body) VALUES ('a')`)
	test.settle("a")
	if why := test.b.status(test.ctx, "shop", test.db, false).Error; why != "" {
		t.Fatalf("a healthy file reports %q", why)
	}
	os.Remove(test.live + "-wal")
	var why string
	for deadline := time.Now().Add(20 * time.Second); why == "" && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		test.b.tick(test.ctx)
		why = test.b.status(test.ctx, "shop", test.db, false).Error
	}
	if !strings.Contains(why, "writing down every change") {
		t.Fatalf("a lost log reports %q", why)
	}
	test.b.letGo("shop")
	test.b.tick(test.ctx)
	test.write(`INSERT INTO notes (body) VALUES ('b')`)
	test.settle("b")
	if why := test.b.status(test.ctx, "shop", test.db, false).Error; why != "" {
		t.Fatalf("after following again: %q", why)
	}
}
