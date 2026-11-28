//go:build docker

package daemon

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/axadrn/moin/protocol"
)

// The promises of backups, against real SQLite, real Postgres (Debian and
// Alpine images) and a real S3. Run with a local SeaweedFS:
//
//	docker run -d -p 127.0.0.1:19100:8333 chrislusf/seaweedfs server -s3
//	MOIN_TEST_S3=http://127.0.0.1:19100 go test -tags docker -run Backup ./internal/daemon/

// backupTest is one app on a test server with a bucket.
type backupTest struct {
	t        *testing.T
	ctx      context.Context
	a        *apps
	b        *backups
	app      string
	bucket   *Bucket
	database protocol.Database
	// yours is your backup key: every copy is encrypted for it too.
	yours *age.X25519Identity
}

func newBackupTest(t *testing.T, app string, compose map[string]any, database protocol.Database) *backupTest {
	t.Helper()
	endpoint := os.Getenv("MOIN_TEST_S3")
	if endpoint == "" {
		t.Skip("set MOIN_TEST_S3 to an S3 endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	t.Cleanup(cancel)
	test := &backupTest{t: t, ctx: ctx, app: app, database: database}
	// The server's backup key lives next to the config.
	t.Setenv("MOIN_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	test.a = newApps(t.TempDir())
	test.b = test.a.backups
	release := test.a.releaseDir(app, 1)
	os.MkdirAll(release, 0o700)
	saveJSON(filepath.Join(release, "compose.json"), compose)
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Databases: []protocol.Database{database}})
	os.WriteFile(filepath.Join(test.a.appDir(app), "current"), []byte("1"), 0o600)
	composeJSON := test.a.composeJSON(app)
	down := func() { exec.Command("docker", "compose", "-p", project(app), "-f", composeJSON, "down", "-v").Run() }
	down()
	t.Cleanup(down)
	if out, err := exec.CommandContext(ctx, "docker", "compose", "-p", project(app), "-f", composeJSON, "up", "-d").CombinedOutput(); err != nil {
		t.Fatalf("compose up: %v %s", err, out)
	}
	// A folder of its own in the bucket for every run.
	test.bucket = &Bucket{Endpoint: endpoint, Region: "us-east-1", Bucket: "moin-test", Path: fmt.Sprintf("run-%d", time.Now().UnixNano()), AccessKey: "x", SecretKey: "y"}
	_, _ = s3Client(test.bucket).CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("moin-test")})
	yours, _ := age.GenerateX25519Identity()
	test.yours = yours
	test.b.configure(Config{Name: "prod", Bucket: test.bucket, BackupKey: &BackupKey{Public: yours.Recipient().String()}, Backups: Backups{Apps: map[string]Schedule{app: {Every: "15m", Keep: "1d"}}}})
	t.Cleanup(func() { test.b.configure(Config{}) })
	return test
}

// copyNow takes the copy that is due, as the tick does once its time
// came, and deletes the old ones.
func (test *backupTest) copyNow() protocol.Copy {
	test.t.Helper()
	time.Sleep(1100 * time.Millisecond) // a new ID
	if err := test.b.copyDue(test.ctx, test.app, test.database, 0); err != nil {
		test.t.Fatalf("the copy failed: %v", err)
	}
	return test.copies()[0]
}

func (test *backupTest) copies() []protocol.Copy {
	test.t.Helper()
	copies, err := test.b.copies(test.ctx, test.app, test.database)
	if err != nil {
		test.t.Fatal(err)
	}
	return copies
}

// restore runs a restore and returns how it ended.
func (test *backupTest) restore(request protocol.BackupRestore) restoreMark {
	test.t.Helper()
	request.ID, request.App, request.Database = fmt.Sprintf("r%d", time.Now().UnixNano()), test.app, test.database.Path
	undo, _ := test.b.restore(test.ctx, request, io.Discard)
	mark := test.stateOf(request.ID)
	if mark.Undo != undo {
		test.t.Fatalf("the restore named the undo %q, its mark %q", undo, mark.Undo)
	}
	return mark
}

// stateOf is how the restore id ended.
func (test *backupTest) stateOf(id string) restoreMark {
	test.t.Helper()
	for _, mark := range test.b.marks() {
		if mark.ID == id {
			return mark
		}
	}
	test.t.Fatalf("no state of the restore %s", id)
	return restoreMark{}
}

// running says every service of the app runs.
func (test *backupTest) running() bool {
	out, _ := exec.Command("docker", "compose", "-p", project(test.app), "-f", test.a.composeJSON(test.app), "ps", "-a", "--format", "{{.State}}").Output()
	return len(strings.Fields(string(out))) > 0 && !strings.Contains(string(out), "exited")
}

// putBroken puts a copy that is not a database in the bucket, newest,
// encrypted as every copy is.
func (test *backupTest) putBroken() {
	test.t.Helper()
	_, folder, _ := test.b.where(test.app, test.database)
	key := folder + "/" + protocol.CopyFile(protocol.NewCopyID(time.Now().Add(time.Minute)), test.database.Kind, false)
	var body bytes.Buffer
	encrypted, err := test.b.encrypt(&body)
	if err != nil {
		test.t.Fatal(err)
	}
	zipped := gzip.NewWriter(encrypted)
	zipped.Write([]byte("this is not a database at all, but it is long enough to look like one"))
	zipped.Close()
	encrypted.Close()
	if _, err := s3Client(test.bucket).PutObject(test.ctx, &s3.PutObjectInput{Bucket: aws.String("moin-test"), Key: aws.String(key), Body: bytes.NewReader(body.Bytes())}); err != nil {
		test.t.Fatal(err)
	}
}

// check runs the daily check and says how it ended, and that it was
// written down.
func (test *backupTest) check() string {
	test.t.Helper()
	before := time.Now().Add(-time.Second)
	test.b.check(test.ctx, test.app, test.database)
	test.b.saveStates()
	fresh := newBackups(test.a)
	status := fresh.status(test.ctx, test.app, test.database, false)
	if status.CheckedAt == nil || status.CheckedAt.Before(before) {
		test.t.Fatalf("the check was not written down: %+v", status)
	}
	fresh.load = test.b.load
	if status.CheckError == "" && status.CheckedCopy != test.copies()[0].ID {
		test.t.Fatalf("the check proved %q, not the newest copy", status.CheckedCopy)
	}
	return status.CheckError
}

func TestBackupSQLite(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0o777)
	live := filepath.Join(dir, "app.db")
	app := fmt.Sprintf("bk-sqlite-%d", time.Now().UnixNano()%1e6)
	compose := map[string]any{"services": map[string]any{"web": map[string]any{"image": "alpine:latest", "command": []string{"sleep", "infinity"}, "init": true}}}
	test := newBackupTest(t, app, compose, protocol.Database{Path: "/data/app.db", HostPath: live})
	// The app opens the file for each write, as a stopped app holds none.
	open := func() *sql.DB {
		t.Helper()
		db, err := sql.Open("sqlite", "file:"+live+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	write := func(sql string) {
		t.Helper()
		db := open()
		defer db.Close()
		if _, err := db.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	rows := func() string {
		t.Helper()
		db := open()
		defer db.Close()
		var list string
		if err := db.QueryRow(`SELECT coalesce(group_concat(body, ','), '') FROM (SELECT body FROM notes ORDER BY id)`).Scan(&list); err != nil {
			t.Fatal(err)
		}
		return list
	}
	write(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`)

	// Copy on the schedule: the first tick takes the first copy, the
	// next one only when its time came.
	write(`INSERT INTO notes (body) VALUES ('a'), ('b')`)
	test.b.tick(test.ctx)
	if copies := test.copies(); len(copies) != 1 || copies[0].Plain {
		t.Fatalf("the first copy: %+v", copies)
	}
	// The same tick proved it: a new database waits no day for its check.
	if status := test.b.status(test.ctx, app, test.database, false); status.CheckedCopy != test.copies()[0].ID || status.CheckError != "" {
		t.Fatalf("the first check: %+v", status)
	}
	// In the bucket it is an age file, which only a key opens: the
	// server's, and yours without the server.
	test.encryptedForBoth(test.copies()[0])
	test.b.tick(test.ctx)
	if copies := test.copies(); len(copies) != 1 {
		t.Fatalf("a copy before its time: %+v", copies)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, ".moin-copy-*")); len(leftovers) != 0 {
		t.Fatalf("the temp file stayed: %v", leftovers)
	}
	first := test.copies()[0]
	write(`INSERT INTO notes (body) VALUES ('c')`)
	second := test.copyNow()

	// Retention: a copy older than the app keeps goes, a plain one from
	// before copies were encrypted too, but never the newest ones.
	_, folder, _ := test.b.where(app, test.database)
	old := time.Now().Add(-3 * day).UTC().Format(protocol.CopyIDLayout)
	s3Client(test.bucket).PutObject(test.ctx, &s3.PutObjectInput{Bucket: aws.String("moin-test"), Key: aws.String(folder + "/" + protocol.CopyFile(old, "", true)), Body: strings.NewReader("old")})
	if listed := test.copies(); listed[len(listed)-1].ID != old || !listed[len(listed)-1].Plain {
		t.Fatalf("a plain copy from before is not listed: %+v", listed)
	}
	write(`INSERT INTO notes (body) VALUES ('d')`)
	third := test.copyNow()
	for _, copy := range test.copies() {
		if copy.ID == old {
			t.Fatal("the old copy stayed")
		}
	}
	if err := test.b.prune(test.ctx, app, test.database, []protocol.Copy{{ID: old, At: time.Now().Add(-3 * day)}}); err != nil {
		t.Fatal(err)
	}

	// Restore to a copy, exact, and the app runs again.
	if state := test.restore(protocol.BackupRestore{Copy: first.ID}); state.State != protocol.RestoreDone {
		t.Fatalf("restore to the first copy: %+v", state)
	}
	if got := rows(); got != "a,b" || !test.running() {
		t.Fatalf("after the restore: %q, running %v", got, test.running())
	}
	// Undo, exact.
	undos, _ := test.b.engine(app, test.database).Undos(test.ctx)
	if len(undos) != 1 {
		t.Fatalf("undos %+v", undos)
	}
	if state := test.restore(protocol.BackupRestore{Undo: undos[0].ID}); state.State != protocol.RestoreDone {
		t.Fatalf("undo: %+v", state)
	}
	if got := rows(); got != "a,b,c,d" {
		t.Fatalf("after the undo: %q", got)
	}

	// A crash before and after the switch: the app runs on a whole
	// database, and the result says which.
	for _, when := range []string{"before", "after"} {
		want := rows()
		if when == "after" {
			want = "a,b,c"
		}
		state := test.killedDuringSQLiteRestore(second.ID, when == "after")
		if got := rows(); got != want || !test.running() || integrityCheck(test.ctx, live) != nil {
			t.Fatalf("crash %s: %q, running %v", when, got, test.running())
		}
		if when == "after" && state.State != protocol.RestoreDone || when == "before" && (state.State != protocol.RestoreFailed || !strings.Contains(state.Error, "as it was before")) {
			t.Fatalf("crash %s: %+v", when, state)
		}
		if exists(live + ".moin-restore") {
			t.Fatalf("crash %s left the restore", when)
		}
	}
	// Two copies in one second never overwrite each other.
	at := time.Now().UTC().Truncate(time.Second)
	one, err1 := test.b.copy(test.ctx, app, test.database, protocol.NewCopyID(at))
	two, err2 := test.b.copy(test.ctx, app, test.database, protocol.NewCopyID(at))
	if err1 != nil || err2 != nil || one == two {
		t.Fatalf("two copies in one second: %s %s %v %v", one, two, err1, err2)
	}
	// No copy while a restore works on the app.
	unlockRestore, _ := tryFileLock(test.b.restoreLock(app))
	if _, err := test.b.copyNow(test.ctx, app, test.database); err == nil || !strings.Contains(err.Error(), "a restore of") {
		t.Fatalf("a copy during a restore: %v", err)
	}
	unlockRestore()
	// A copy or an undo that is not there is refused before anything is
	// written down.
	marks := len(test.b.marks())
	for _, request := range []protocol.BackupRestore{{Copy: "20200101T000000Z-123456"}, {Undo: "20200101T000000Z-123456"}} {
		request.ID, request.App = "nothing", app
		if _, err := test.b.restore(test.ctx, request, io.Discard); err == nil || !strings.Contains(err.Error(), "has no") || len(test.b.marks()) != marks {
			t.Fatalf("a restore to what is not there: %v", err)
		}
	}
	if state := test.restore(protocol.BackupRestore{Copy: third.ID}); state.State != protocol.RestoreDone || rows() != "a,b,c,d" {
		t.Fatalf("back to the third copy: %+v", state)
	}

	// The daily check passes, and fails loudly on a broken copy.
	if why := test.check(); why != "" {
		t.Fatalf("the check failed: %s", why)
	}
	test.putBroken()
	if why := test.check(); !strings.Contains(why, "did not restore") {
		t.Fatalf("a broken copy passed the check: %q", why)
	}

	// Download: the copy in the bucket opens with the SQLite driver.
	from := mustCopy(t, test, third.ID)
	if from.size == 0 {
		t.Fatal("the copy carries no note of its size")
	}
	downloaded := filepath.Join(t.TempDir(), "copy.db")
	out, err := os.Create(downloaded)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(gunzipCopy(from, out), out.Close()); err != nil {
		t.Fatal(err)
	}
	opened, _ := sql.Open("sqlite", "file:"+downloaded+"?mode=ro")
	defer opened.Close()
	var n int
	if err := opened.QueryRow(`SELECT count(*) FROM notes`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("the download: %d rows, %v", n, err)
	}
	// A deploy takes a copy just before it, and names it.
	count := len(test.copies())
	if ids, err := test.b.copyBefore(test.ctx, app); err != nil || len(ids) != 1 || len(test.copies()) != count+1 {
		t.Fatalf("the copy before a deploy: %v %v", ids, err)
	}
	// A copy from the future, after the clock jumped, does not stop the
	// copies: the newest real one counts (the broken copy above is a
	// minute ahead).
	// The failed check above would hold the copy back for a while: it is
	// not what this part tests.
	test.b.mu.Lock()
	test.b.state(app, test.database).CheckError = ""
	test.b.state(app, test.database).LastCopy = time.Now().Add(time.Hour)
	test.b.mu.Unlock()
	test.b.tick(test.ctx)
	test.b.mu.Lock()
	copied := test.b.state(app, test.database).LastCopy
	test.b.mu.Unlock()
	if copied.After(time.Now()) {
		t.Fatalf("the copies wait for one from the future: %v", copied)
	}
}

func TestBackupPostgres(t *testing.T) {
	for _, image := range []string{"postgres:17", "postgres:17-alpine"} {
		t.Run(image, func(t *testing.T) { testBackupPostgres(t, image) })
	}
}

func testBackupPostgres(t *testing.T, image string) {
	app := fmt.Sprintf("bk-pg-%d", time.Now().UnixNano()%1e6)
	compose := map[string]any{"services": map[string]any{
		"web": map[string]any{"image": "alpine:latest", "command": []string{"sleep", "infinity"}, "init": true},
		"db":  map[string]any{"image": image, "environment": map[string]string{"POSTGRES_USER": "app", "POSTGRES_DB": "shop", "POSTGRES_PASSWORD": "secret"}},
	}}
	database := protocol.Database{Path: protocol.PostgresKey("db"), Kind: protocol.DatabasePostgres, Service: "db"}
	test := newBackupTest(t, app, compose, database)
	var target pgTarget
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(time.Second) {
		var err error
		target, err = test.a.postgres(test.ctx, app, database)
		if err == nil {
			if _, err = target.psql(test.ctx, `SELECT 1;`, nil, false); err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
	}
	sql := func(script string) string {
		t.Helper()
		out, err := target.psql(test.ctx, script, nil, false)
		if err != nil {
			t.Fatalf("%s: %v", script, err)
		}
		return strings.TrimSpace(out)
	}
	rows := func() string { return sql(`SELECT coalesce(string_agg(body, ',' ORDER BY id), '') FROM notes;`) }
	sql(`CREATE TABLE notes (id serial PRIMARY KEY, body text); INSERT INTO notes (body) VALUES ('a'), ('b');`)
	sql(`CREATE ROLE keeper; ALTER DATABASE shop OWNER TO keeper;`)
	test.b.tick(test.ctx)
	first := test.copies()[0]
	sql(`INSERT INTO notes (body) VALUES ('c');`)
	second := test.copyNow()

	if state := test.restore(protocol.BackupRestore{Copy: first.ID}); state.State != protocol.RestoreDone {
		t.Fatalf("restore to the first copy: %+v", state)
	}
	if got := rows(); got != "a,b" || !test.running() {
		t.Fatalf("after the restore: %q, running %v", got, test.running())
	}
	// The restored database keeps the owner of the live one.
	if owner := sql(`SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = 'shop';`); owner != "keeper" {
		t.Fatalf("the owner after the restore: %q", owner)
	}
	// The sequence came back too: a new row gets the next ID.
	sql(`INSERT INTO notes (body) VALUES ('x'); DELETE FROM notes WHERE body = 'x';`)
	undos, _ := test.b.engine(app, database).Undos(test.ctx)
	if len(undos) != 1 {
		t.Fatalf("undos %+v", undos)
	}
	if state := test.restore(protocol.BackupRestore{Undo: undos[0].ID}); state.State != protocol.RestoreDone {
		t.Fatalf("undo: %+v", state)
	}
	if got := rows(); got != "a,b,c" {
		t.Fatalf("after the undo: %q", got)
	}

	for _, step := range []string{"before", "after"} {
		want := "a,b,c"
		if step == "after" {
			want = "a,b"
		}
		state := test.killedDuringPostgresRestore(first.ID, step == "after")
		if got := rows(); got != want || !test.running() {
			t.Fatalf("crash at %s: %q, running %v", step, got, test.running())
		}
		if step == "after" && state.State != protocol.RestoreDone || step == "before" && (state.State != protocol.RestoreFailed || !strings.Contains(state.Error, "as it was before")) {
			t.Fatalf("crash at %s: %+v", step, state)
		}
		if sql(`SELECT count(*) FROM pg_database WHERE datname LIKE 'moin\_restore\_%';`) != "0" {
			t.Fatalf("crash at %s left the restore", step)
		}
		if step == "after" {
			test.restore(protocol.BackupRestore{Copy: second.ID})
		}
	}
	if got := rows(); got != "a,b,c" {
		t.Fatalf("back to the second copy: %q", got)
	}

	if why := test.check(); why != "" {
		t.Fatalf("the check failed: %s", why)
	}
	test.putBroken()
	if why := test.check(); !strings.Contains(why, "did not restore") {
		t.Fatalf("a broken copy passed the check: %q", why)
	}

	// Download: the copy in the bucket lists with pg_restore.
	from := mustCopy(t, test, second.ID)
	if from.size == 0 {
		t.Fatal("the copy carries no note of its size")
	}
	var list bytes.Buffer
	err := target.exec(test.ctx, from.copy, &list, "pg_restore", "--list")
	if err != nil || !strings.Contains(list.String(), "TABLE public notes") {
		t.Fatalf("pg_restore --list: %v %s", err, list.String())
	}
	_, _ = io.Copy(io.Discard, &list)

	// Every database of the service is backed up, not only the one
	// POSTGRES_DB names: each its own copies, restore, undo and check.
	sql(`CREATE DATABASE analytics;`)
	analytics := target
	analytics.database = "analytics"
	other := func(script string) string {
		t.Helper()
		out, err := analytics.psql(test.ctx, script, nil, false)
		if err != nil {
			t.Fatalf("%s: %v", script, err)
		}
		return strings.TrimSpace(out)
	}
	other(`CREATE TABLE visits (id serial PRIMARY KEY, page text); INSERT INTO visits (page) VALUES ('/');`)
	paths := []string{}
	for _, database := range test.b.databases(test.ctx, app) {
		paths = append(paths, database.Path)
	}
	if strings.Join(paths, ",") != "postgres-db,postgres-db/analytics" {
		t.Fatalf("the databases of the service: %v", paths)
	}
	shop := test.database
	test.database = protocol.Database{Path: "postgres-db/analytics", Kind: protocol.DatabasePostgres, Service: "db", Name: "analytics"}
	defer func() { test.database = shop }()
	// The next look at the databases finds the new one.
	test.b.foundAt = time.Time{}
	test.b.tick(test.ctx)
	if copies := test.copies(); len(copies) != 1 {
		t.Fatalf("the first copy of analytics: %+v", copies)
	}
	if listed, err := test.listed(); err != nil || len(listed) != 2 || listed[1].Database != "postgres-db/analytics" {
		t.Fatalf("the list: %+v %v", listed, err)
	}
	firstVisit := test.copies()[0]
	other(`INSERT INTO visits (page) VALUES ('/about');`)
	test.copyNow()
	visits := func() string { return other(`SELECT string_agg(page, ',' ORDER BY id) FROM visits;`) }
	before := rows()
	if state := test.restore(protocol.BackupRestore{Copy: firstVisit.ID}); state.State != protocol.RestoreDone {
		t.Fatalf("restore analytics: %+v", state)
	}
	if got := visits(); got != "/" || rows() != before || !test.running() {
		t.Fatalf("after the restore of analytics: %q, shop %q", got, rows())
	}
	undos, _ = test.b.engine(app, test.database).Undos(test.ctx)
	if len(undos) != 1 {
		t.Fatalf("undos of analytics %+v", undos)
	}
	if state := test.restore(protocol.BackupRestore{Undo: undos[0].ID}); state.State != protocol.RestoreDone || visits() != "/,/about" {
		t.Fatalf("undo analytics: %+v %q", state, visits())
	}
	if err := test.b.engine(app, test.database).Check(test.ctx, mustCopy(t, test, firstVisit.ID)); err != nil {
		t.Fatalf("the check of analytics: %v", err)
	}
}

// mustCopy opens a copy of the test's database in the bucket, decrypted
// with the server's key.
func mustCopy(t *testing.T, test *backupTest, id string) source {
	t.Helper()
	copy := protocol.Copy{ID: id}
	body, size, err := test.b.readCopy(test.ctx, test.app, test.database, copy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { body.Close() })
	plain, err := test.b.decrypt(copy, body)
	if err != nil {
		t.Fatal(err)
	}
	return source{copy: plain, size: size}
}

// listed is what `moin backups` shows of the app, as the test's config
// has it.
func (test *backupTest) listed() ([]protocol.DatabaseBackup, error) {
	statuses := []protocol.DatabaseBackup{}
	for _, database := range test.b.databases(test.ctx, test.app) {
		statuses = append(statuses, test.b.status(test.ctx, test.app, database, true))
	}
	return statuses, nil
}

// encryptedForBoth checks a copy as it lies in the bucket: not readable
// as it is, and opened by the server's key and by yours, each alone, and
// by no other key.
func (test *backupTest) encryptedForBoth(copy protocol.Copy) {
	test.t.Helper()
	read := func() []byte {
		body, _, err := test.b.readCopy(test.ctx, test.app, test.database, copy)
		if err != nil {
			test.t.Fatal(err)
		}
		defer body.Close()
		all, _ := io.ReadAll(body)
		return all
	}
	raw := read()
	if !bytes.HasPrefix(raw, []byte("age-encryption.org/v1")) || bytes.Contains(raw, []byte("SQLite format 3")) || bytes.Contains(raw, []byte("PGDMP")) {
		test.t.Fatalf("the copy in the bucket is not an age file: %q", raw[:min(len(raw), 40)])
	}
	server, err := serverKey()
	if err != nil {
		test.t.Fatal(err)
	}
	stranger, _ := age.GenerateX25519Identity()
	for name, key := range map[string]age.Identity{"the server's key": server, "your key": test.yours} {
		plain, err := DecryptCopy(bytes.NewReader(raw), key)
		if err != nil {
			test.t.Fatalf("%s does not open the copy: %v", name, err)
		}
		if n, err := io.Copy(io.Discard, plain); err != nil || n == 0 {
			test.t.Fatalf("%s reads %d bytes of the copy: %v", name, n, err)
		}
	}
	if _, err := DecryptCopy(bytes.NewReader(raw), stranger); !errors.Is(err, errWrongKey) {
		test.t.Fatalf("a stranger's key: %v", err)
	}
}

// killed writes down a restore as running and hands back a mark for a
// daemon that stops now.
func (test *backupTest) killed() restoreMark {
	test.t.Helper()
	mark := restoreMark{ID: fmt.Sprintf("r%d", time.Now().UnixNano()), App: test.app, Database: test.database.Path, Undo: protocol.NewCopyID(time.Now()), State: protocol.RestoreRunning}
	if err := writeState(test.b.markFile(mark.ID), mark); err != nil {
		test.t.Fatal(err)
	}
	return mark
}

// killedDuringSQLiteRestore leaves what a daemon stopped just before or
// just after the switch leaves, and starts the daemon again.
func (test *backupTest) killedDuringSQLiteRestore(copy string, switched bool) restoreMark {
	test.t.Helper()
	live, built := test.database.HostPath, test.database.HostPath+".moin-restore"
	from := mustCopy(test.t, test, copy)
	root, name, err := databaseFolder(test.database)
	if err != nil {
		test.t.Fatal(err)
	}
	defer root.Close()
	out, err := os.Create(built)
	if err != nil {
		test.t.Fatal(err)
	}
	if err := errors.Join(gunzipCopy(from, out), out.Close()); err != nil {
		test.t.Fatal(err)
	}
	if _, err := test.b.apps.stopServices(test.ctx, test.app); err != nil {
		test.t.Fatal(err)
	}
	if err := checkpoint(test.ctx, root, name); err != nil {
		test.t.Fatal(err)
	}
	mark := test.killed()
	if err := os.Link(live, undoFile(live, mark.Undo)); err != nil {
		test.t.Fatal(err)
	}
	if switched {
		if err := os.Rename(built, live); err != nil {
			test.t.Fatal(err)
		}
	}
	test.b.recover(test.ctx)
	return test.stateOf(mark.ID)
}

// killedDuringPostgresRestore leaves what a daemon stopped just before
// or just after the switch leaves, and starts the daemon again.
func (test *backupTest) killedDuringPostgresRestore(copy string, switched bool) restoreMark {
	test.t.Helper()
	target, err := test.b.apps.postgres(test.ctx, test.app, test.database)
	if err != nil {
		test.t.Fatal(err)
	}
	built := moinName("restore", target.database)
	from := mustCopy(test.t, test, copy)
	if err := load(test.ctx, target, built, from.copy); err != nil {
		test.t.Fatal(err)
	}
	if _, err := test.b.apps.stopServices(test.ctx, test.app, test.b.otherServices(test.app, test.database.Service)...); err != nil {
		test.t.Fatal(err)
	}
	mark := test.killed()
	if switched {
		if err := swap(test.ctx, target, target.database, undoName(target.database, mark.Undo), built); err != nil {
			test.t.Fatal(err)
		}
	}
	test.b.recover(test.ctx)
	return test.stateOf(mark.ID)
}
