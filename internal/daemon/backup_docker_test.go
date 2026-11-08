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
	test.b.configure(protocol.BackupConfig{Bucket: test.bucket, Schedules: map[string]protocol.Schedule{app: {Every: 15, Keep: 1}}})
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
	_ = test.b.restore(test.ctx, request)
	return test.stateOf(request.ID)
}

// stateOf is how the restore id ended.
func (test *backupTest) stateOf(id string) protocol.RestoreState {
	test.t.Helper()
	for _, state := range test.b.restoreStates(test.app) {
		if state.ID == id {
			return state
		}
	}
	test.t.Fatalf("no state of the restore %s", id)
	return protocol.RestoreState{}
}

// running says every service of the app runs.
func (test *backupTest) running() bool {
	out, _ := exec.Command("docker", "compose", "-p", project(test.app), "-f", test.a.composeFile(test.app), "ps", "-a", "--format", "{{.State}}").Output()
	return len(strings.Fields(string(out))) > 0 && !strings.Contains(string(out), "exited")
}

// putBroken puts a Postgres copy that is not a dump in the bucket, newest.
func (test *backupTest) putBroken() {
	test.t.Helper()
	_, folder, _ := test.b.where(test.app, test.database)
	key := folder + "/" + protocol.CopyFile(protocol.NewCopyID(time.Now().Add(time.Minute)))
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
	// The app opens the file for each write, as an app that opens and
	// closes SQLite per request does.
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

	// Following the file: the round has Litestream write every change,
	// and takes no copy.
	write(`INSERT INTO notes (body) VALUES ('a'), ('b')`)
	test.b.tick(test.ctx)
	waitRow(t, test, "a")
	_, folder, _ := test.b.where(app, test.database)
	if listed := folders(test.ctx, s3Client(test.bucket), "moin-test", folder+"/"); slices.Contains(listed, folder+"/copies/") {
		t.Fatalf("a copy of SQLite: %v", listed)
	}

	// Back to a minute, exact, and the app runs again.
	write(`INSERT INTO notes (body) VALUES ('c')`)
	waitRow(t, test, "c")
	time.Sleep(1100 * time.Millisecond)
	noted := time.Now().UTC()
	time.Sleep(1100 * time.Millisecond)
	write(`INSERT INTO notes (body) VALUES ('d'); DELETE FROM notes WHERE body = 'a'`)
	waitRow(t, test, "d")
	if state := test.restore(protocol.BackupRestore{At: &noted}); state.State != protocol.RestoreDone {
		t.Fatalf("restore to a minute: %+v", state)
	}
	if got := rows(); got != "a,b,c" || !test.running() {
		t.Fatalf("after the restore to a minute: %q, running %v", got, test.running())
	}

	// Undo, exact.
	undos, _ := test.b.engine(app, test.database).Undos(test.ctx)
	if len(undos) != 1 {
		t.Fatalf("undos %+v", undos)
	}
	if state := test.restore(protocol.BackupRestore{Undo: undos[0].ID}); state.State != protocol.RestoreDone {
		t.Fatalf("undo: %+v", state)
	}
	if got := rows(); got != "b,c,d" {
		t.Fatalf("after the undo: %q", got)
	}

	// A restore starts a new generation from the restored file; the one
	// before stays in the bucket until it is older than the app keeps.
	test.b.tick(test.ctx)
	write(`INSERT INTO notes (body) VALUES ('e')`)
	waitRow(t, test, "e")
	if generations := folders(test.ctx, s3Client(test.bucket), "moin-test", folder+"/litestream/"); len(generations) != 2 {
		t.Fatalf("generations %v", generations)
	}

	// A crash before and after the switch: the app runs on a whole
	// database, and the result says which.
	for _, when := range []string{"before", "after"} {
		want := rows()
		state := test.killedDuringSQLiteRestore(when == "after")
		if got := rows(); got != want || !test.running() || integrityCheck(test.ctx, live) != nil {
			t.Fatalf("crash %s: %q, running %v", when, got, test.running())
		}
		if when == "after" && state.State != protocol.RestoreDone || when == "before" && (state.State != protocol.RestoreFailed || !strings.Contains(state.Error, "as it was before")) {
			t.Fatalf("crash %s: %+v", when, state)
		}
		if exists(live + ".moin-restore") {
			t.Fatalf("crash %s left the restore", when)
		}
		test.b.tick(test.ctx)
		write(`INSERT INTO notes (body) VALUES ('f-` + when + `')`)
		waitRow(t, test, "f-"+when)
	}

	// No edit while a restore works on the app.
	test.b.restoring[app]++
	if _, err := test.a.query(test.ctx, protocol.DBQuery{DBRequest: protocol.DBRequest{App: app}, SQL: `DELETE FROM notes`, Write: true}); err == nil || !strings.Contains(err.Error(), "a restore of") {
		t.Fatalf("an edit during a restore: %v", err)
	}
	test.b.restoring[app]--

	// The daily check passes.
	if why := test.check(); why != "" {
		t.Fatalf("the check failed: %s", why)
	}

	// The download: the newest state, gzipped, and it opens.
	copy, err := test.b.download(test.ctx, protocol.BackupRequest{App: app, Database: test.database.Path})
	if err != nil {
		t.Fatal(err)
	}
	object, err := s3Client(test.bucket).GetObject(test.ctx, &s3.GetObjectInput{Bucket: aws.String("moin-test"), Key: aws.String(protocol.DownloadFile(folder, copy.ID))})
	if err != nil {
		t.Fatal(err)
	}
	defer object.Body.Close()
	zipped, err := gzip.NewReader(object.Body)
	if err != nil {
		t.Fatal(err)
	}
	downloaded := filepath.Join(t.TempDir(), "download.db")
	body, _ := io.ReadAll(zipped)
	os.WriteFile(downloaded, body, 0o600)
	if err := integrityCheck(test.ctx, downloaded); err != nil {
		t.Fatalf("the download: %v", err)
	}

	// An edit through moin remembers the moment just before it, and
	// takes no copy.
	time.Sleep(1100 * time.Millisecond)
	result, err := test.a.query(test.ctx, protocol.DBQuery{DBRequest: protocol.DBRequest{App: app, Database: "/data/app.db"}, SQL: `INSERT INTO notes (body) VALUES ('g')`, Write: true})
	if err != nil || result.Before == nil {
		t.Fatalf("the moment before an edit: %+v %v", result.Before, err)
	}

	// After a deploy, Litestream starts over: the next round follows the
	// file again, on the same generation, and the history is kept.
	waitRow(t, test, "g")
	kept := generation(live)
	since, _ := test.b.reach(test.ctx, app, test.database)
	test.b.letGo(app)
	write(`INSERT INTO notes (body) VALUES ('h')`)
	test.b.tick(test.ctx)
	write(`INSERT INTO notes (body) VALUES ('i')`)
	waitRow(t, test, "h")
	waitRow(t, test, "i")
	if generation(live) != kept {
		t.Fatal("following again changed the generation")
	}
	if now, _ := test.b.reach(test.ctx, app, test.database); now == nil || !now.Equal(*since) {
		t.Fatalf("the history went: %v, was %v", now, since)
	}
}

// waitRow runs rounds until the row with this body is in the bucket.
func waitRow(t *testing.T, test *backupTest, body string) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		test.b.tick(test.ctx)
		if reached(test.ctx, test.b, test.app, test.database, body) {
			return
		}
	}
	t.Fatalf("the row %s never reached the bucket", body)
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
	if why := test.check(); !strings.Contains(why, "the daily check failed") {
		t.Fatalf("a broken copy passed the check: %q", why)
	}

	// Download: the copy in the bucket lists with pg_restore.
	from, body, err := test.b.readCopy(test.ctx, app, database, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if from.size == 0 {
		t.Fatal("the copy carries no note of its size")
	}
	var list bytes.Buffer
	err = target.exec(test.ctx, from.copy, &list, "pg_restore", "--list")
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
	if err := test.b.engine(app, test.database).Check(test.ctx); err != nil {
		t.Fatalf("the check of analytics: %v", err)
	}
}

// mustCopy opens a copy of the test's database in the bucket.
func mustCopy(t *testing.T, test *backupTest, id string) source {
	t.Helper()
	from, body, err := test.b.readCopy(test.ctx, test.app, test.database, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { body.Close() })
	return from
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
// just after the switch leaves, and starts the daemon again. It builds
// the newest state, so the data stays the same either way.
func (test *backupTest) killedDuringSQLiteRestore(switched bool) protocol.RestoreState {
	test.t.Helper()
	live, built := test.database.HostPath, test.database.HostPath+".moin-restore"
	engine := &sqliteEngine{b: test.b, app: test.app, database: test.database}
	if err := engine.restoreAt(test.ctx, time.Time{}, built); err != nil {
		test.t.Fatal(err)
	}
	if _, err := test.b.apps.stopServices(test.ctx, test.app); err != nil {
		test.t.Fatal(err)
	}
	test.b.unwatch(live)
	if err := checkpoint(test.ctx, live); err != nil {
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
func (test *backupTest) killedDuringPostgresRestore(copy string, switched bool) protocol.RestoreState {
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
	engine := &postgresEngine{b: test.b, app: test.app, database: test.database}
	if _, err := test.b.apps.stopServices(test.ctx, test.app, engine.others()...); err != nil {
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
