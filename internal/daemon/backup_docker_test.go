//go:build docker

package daemon

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	bucket   *protocol.Bucket
	database protocol.Database
	mu       sync.Mutex
	sent     []protocol.Backups
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
	test.a = newApps(t.TempDir(), nil)
	test.b = newBackups(test.a, func(kind protocol.MessageType, _ string, data any) {
		if backups, ok := data.(protocol.Backups); ok && kind == protocol.TypeBackupStatus {
			test.mu.Lock()
			test.sent = append(test.sent, backups)
			test.mu.Unlock()
		}
	})
	test.a.backups = test.b
	release := test.a.releaseDir(app, 1)
	os.MkdirAll(release, 0o700)
	saveJSON(filepath.Join(release, "compose.json"), compose)
	saveJSON(filepath.Join(release, "detected.json"), protocol.Detected{Databases: []protocol.Database{database}})
	os.WriteFile(filepath.Join(test.a.appDir(app), "current"), []byte("1"), 0o600)
	os.WriteFile(filepath.Join(test.a.appDir(app), "app-id"), []byte(fmt.Sprintf("id-%d", time.Now().UnixNano())), 0o600)
	composeFile := test.a.composeFile(app)
	down := func() { exec.Command("docker", "compose", "-p", project(app), "-f", composeFile, "down", "-v").Run() }
	down()
	t.Cleanup(down)
	if out, err := exec.CommandContext(ctx, "docker", "compose", "-p", project(app), "-f", composeFile, "up", "-d").CombinedOutput(); err != nil {
		t.Fatalf("compose up: %v %s", err, out)
	}
	test.bucket = &protocol.Bucket{Endpoint: endpoint, Region: "us-east-1", Bucket: "moin-test", Workspace: "ws-" + app, AccessKey: "x", SecretKey: "y"}
	_, _ = s3Client(test.bucket).CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("moin-test")})
	test.b.configure(protocol.BackupConfig{Bucket: test.bucket, Schedules: map[string]protocol.Schedule{app: {Every: 15, Keep: 1, EveryChange: true}}})
	t.Cleanup(func() { test.b.configure(protocol.BackupConfig{}) })
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
func (test *backupTest) restore(request protocol.BackupRestore) protocol.RestoreState {
	test.t.Helper()
	request.ID, request.App, request.Database = fmt.Sprintf("r%d", time.Now().UnixNano()), test.app, test.database.Path
	err := test.b.restore(test.ctx, request)
	if err == errCrash {
		test.b.crash = nil
		test.b.recover(test.ctx)
	}
	for _, state := range test.b.restoreStates(test.app) {
		if state.ID == request.ID {
			return state
		}
	}
	test.t.Fatalf("no state of the restore %s", request.ID)
	return protocol.RestoreState{}
}

// running says every service of the app runs.
func (test *backupTest) running() bool {
	out, _ := exec.Command("docker", "compose", "-p", project(test.app), "-f", test.a.composeFile(test.app), "ps", "-a", "--format", "{{.State}}").Output()
	return len(strings.Fields(string(out))) > 0 && !strings.Contains(string(out), "exited")
}

// putBroken puts a copy that is not a database in the bucket, newest.
func (test *backupTest) putBroken() {
	test.t.Helper()
	_, folder, _ := test.b.where(test.app, test.database)
	key := folder + "/" + protocol.CopyFile(protocol.NewCopyID(time.Now().Add(time.Minute)), test.database.Postgres())
	var body bytes.Buffer
	zipped := gzip.NewWriter(&body)
	zipped.Write([]byte("this is not a database at all, but it is long enough to look like one"))
	zipped.Close()
	if _, err := s3Client(test.bucket).PutObject(test.ctx, &s3.PutObjectInput{Bucket: aws.String("moin-test"), Key: aws.String(key), Body: bytes.NewReader(body.Bytes())}); err != nil {
		test.t.Fatal(err)
	}
}

// checks runs the daily check and says how it ended, and that the
// hub heard of it.
func (test *backupTest) check() string {
	test.t.Helper()
	time.Sleep(100 * time.Millisecond) // the report of the last restore
	test.mu.Lock()
	test.sent = nil
	test.mu.Unlock()
	test.b.checkAll(test.ctx)
	test.mu.Lock()
	defer test.mu.Unlock()
	if len(test.sent) != 1 || test.sent[0].Databases[0].CheckedAt == nil {
		test.t.Fatalf("the check was not reported: %+v", test.sent)
	}
	return test.sent[0].Databases[0].CheckError
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
	if copies := test.copies(); len(copies) != 1 {
		t.Fatalf("the first copy: %+v", copies)
	}
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

	// Retention: a copy older than the app keeps goes, but never the
	// newest one.
	_, folder, _ := test.b.where(app, test.database)
	old := time.Now().Add(-3 * day).UTC().Format(protocol.CopyIDLayout)
	s3Client(test.bucket).PutObject(test.ctx, &s3.PutObjectInput{Bucket: aws.String("moin-test"), Key: aws.String(folder + "/" + protocol.CopyFile(old, false)), Body: strings.NewReader("old")})
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

	// Every change: back to a minute, exact.
	time.Sleep(2 * time.Second)
	test.b.tick(test.ctx) // Litestream follows the restored file afresh
	write(`INSERT INTO notes (body) VALUES ('e')`)
	waitChange(t, test, time.Now())
	time.Sleep(1100 * time.Millisecond)
	noted := time.Now().UTC()
	time.Sleep(1100 * time.Millisecond)
	write(`INSERT INTO notes (body) VALUES ('f'); DELETE FROM notes WHERE body = 'a'`)
	waitChange(t, test, noted)
	if state := test.restore(protocol.BackupRestore{At: &noted}); state.State != protocol.RestoreDone {
		t.Fatalf("restore to a minute: %+v", state)
	}
	if got := rows(); got != "a,b,c,d,e" {
		t.Fatalf("after the restore to a minute: %q", got)
	}
	// Every change starts a new generation from the restored file; the
	// one before stays in the bucket until it is older than the app keeps.
	test.b.tick(test.ctx)
	_, folder, _ = test.b.where(app, test.database)
	if generations := folders(test.ctx, s3Client(test.bucket), "moin-test", folder+"/litestream/"); len(generations) != 2 {
		t.Fatalf("generations %v", generations)
	}

	// A crash before and after the switch: the app runs on a whole
	// database, and the result says which.
	for _, when := range []string{"before", "after"} {
		want := rows()
		if when == "after" {
			want = "a,b,c"
		}
		test.b.crash = func(at string) bool { return at == when }
		state := test.restore(protocol.BackupRestore{Copy: second.ID})
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
	// No edit while a restore works on the app.
	test.b.restoring[app]++
	if _, err := test.a.query(test.ctx, protocol.DBQuery{DBRequest: protocol.DBRequest{App: app}, SQL: `DELETE FROM notes`, Write: true}); err == nil || !strings.Contains(err.Error(), "a restore of") {
		t.Fatalf("an edit during a restore: %v", err)
	}
	test.b.restoring[app]--
	if state := test.restore(protocol.BackupRestore{Copy: third.ID}); state.State != protocol.RestoreDone || rows() != "a,b,c,d" {
		t.Fatalf("back to the third copy: %+v", state)
	}

	// The daily check passes, and fails loudly on a broken copy.
	if why := test.check(); why != "" {
		t.Fatalf("the check failed: %s", why)
	}
	test.putBroken()
	if why := test.check(); !strings.Contains(why, "the daily check failed") {
		t.Fatalf("a broken copy passed the check: %q", why)
	}

	// Download: the copy in the bucket opens with the SQLite driver.
	body, err := test.b.object(test.ctx, app, test.database, third.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	downloaded := filepath.Join(t.TempDir(), "copy.db")
	if err := (&sqliteEngine{}).build(test.ctx, source{copy: body}, downloaded); err != nil {
		t.Fatal(err)
	}
	body.Close()
	opened, _ := sql.Open("sqlite", "file:"+downloaded+"?mode=ro")
	defer opened.Close()
	var n int
	if err := opened.QueryRow(`SELECT count(*) FROM notes`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("the download: %d rows, %v", n, err)
	}
	// An edit through moin takes a copy just before it: "Before your
	// edit" is that copy.
	time.Sleep(1100 * time.Millisecond)
	result, err := test.a.query(test.ctx, protocol.DBQuery{DBRequest: protocol.DBRequest{App: app, Database: "/data/app.db"}, SQL: `INSERT INTO notes (body) VALUES ('g')`, Write: true})
	if err != nil || result.Before == nil || !slices.ContainsFunc(test.copies(), func(copy protocol.Copy) bool { return copy.At.Equal(*result.Before) }) {
		t.Fatalf("the copy before an edit: %+v %v", result.Before, err)
	}
	// A copy from the future, after the clock jumped, does not stop the
	// copies: the newest real one counts (the broken copy above is a
	// minute ahead).
	test.b.mu.Lock()
	test.b.state(app, test.database).copied = time.Now().Add(time.Hour)
	test.b.mu.Unlock()
	test.b.tick(test.ctx)
	test.b.mu.Lock()
	copied := test.b.state(app, test.database).copied
	test.b.mu.Unlock()
	if copied.After(time.Now()) {
		t.Fatalf("the copies wait for one from the future: %v", copied)
	}
	// Turning Every change off removes what it wrote down.
	test.b.configure(protocol.BackupConfig{Bucket: test.bucket, Schedules: map[string]protocol.Schedule{app: {Every: 15, Keep: 1}}})
	test.b.tick(test.ctx)
	if since, _ := test.b.reach(test.ctx, app, test.database); since != nil || exists(litestreamState(live)) {
		t.Fatal("Every change stayed after it was turned off")
	}
}

// waitChange waits until Every change has a change after after.
func waitChange(t *testing.T, test *backupTest, after time.Time) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		test.b.tick(test.ctx)
		if _, until := test.b.reach(test.ctx, test.app, test.database); until != nil && until.After(after) {
			return
		}
	}
	t.Fatal("Every change wrote nothing down")
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
	if state := test.restore(protocol.BackupRestore{At: &first.At}); state.State != protocol.RestoreFailed {
		t.Fatalf("Postgres went back to a minute: %+v", state)
	}

	for _, step := range []string{"before", "after"} {
		want := "a,b,c"
		if step == "after" {
			want = "a,b"
		}
		test.b.crash = func(at string) bool { return at == step }
		state := test.restore(protocol.BackupRestore{Copy: first.ID})
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
	if why := test.check(); !strings.Contains(why, "the daily check failed") {
		t.Fatalf("a broken copy passed the check: %q", why)
	}

	// Download: the copy in the bucket lists with pg_restore.
	body, err := test.b.object(test.ctx, app, database, second.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	var list bytes.Buffer
	err = target.exec(test.ctx, body, &list, "pg_restore", "--list")
	body.Close()
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
	if strings.Join(paths, ",") != "postgres-db,postgres-db/analytics,postgres-db/postgres" {
		t.Fatalf("the databases of the service: %v", paths)
	}
	shop := test.database
	test.database = protocol.Database{Path: "postgres-db/analytics", Kind: protocol.DatabasePostgres, Service: "db", Name: "analytics"}
	defer func() { test.database = shop }()
	test.b.tick(test.ctx)
	if copies := test.copies(); len(copies) != 1 {
		t.Fatalf("the first copy of analytics: %+v", copies)
	}
	if listed := test.b.list(test.ctx, protocol.BackupRequest{App: app}); len(listed.Databases) != 3 || len(listed.Databases[1].Copies) != 1 || listed.Databases[1].Database != "postgres-db/analytics" {
		t.Fatalf("the list: %+v", listed.Databases)
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

// mustCopy opens a copy of the test's database in the bucket.
func mustCopy(t *testing.T, test *backupTest, id string) io.Reader {
	t.Helper()
	body, err := test.b.object(test.ctx, test.app, test.database, id, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { body.Close() })
	return body
}
