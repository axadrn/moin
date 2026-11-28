//go:build docker

package daemon

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// The promises of backups against real MySQL and real MariaDB: a copy, a
// restore, its undo, a restore that was cut off, the daily check, and a
// broken copy that must not pass it. Run like the other backup tests.
func TestBackupMySQL(t *testing.T) {
	for _, image := range []string{"mysql:8", "mariadb:11"} {
		t.Run(image, func(t *testing.T) { testBackupMySQL(t, image) })
	}
}

func testBackupMySQL(t *testing.T, image string) {
	app := fmt.Sprintf("bk-my-%d", time.Now().UnixNano()%1e6)
	// The app's own user makes the tables and the view, as its migrations
	// would, and the trigger is its too.
	compose := map[string]any{"services": map[string]any{
		"web": map[string]any{"image": "alpine:latest", "command": []string{"sleep", "infinity"}, "init": true},
		"db":  map[string]any{"image": image, "environment": map[string]string{"MYSQL_ROOT_PASSWORD": "s3cret\" x", "MYSQL_DATABASE": "shop", "MYSQL_USER": "app", "MYSQL_PASSWORD": "apppass"}},
	}}
	database := protocol.Database{Path: protocol.MySQLKey("db"), Kind: protocol.DatabaseMySQL, Service: "db"}
	test := newBackupTest(t, app, compose, database)
	engine := &mysqlEngine{b: test.b, app: app, database: database}
	// The image starts a MySQL that only sets things up first: wait for
	// the real one, which listens on TCP.
	var target mysqlTarget
	for deadline := time.Now().Add(3 * time.Minute); ; time.Sleep(time.Second) {
		var err error
		if target, err = engine.target(test.ctx); err == nil {
			tcp := target
			tcp.host = "127.0.0.1"
			if tcp.answers(test.ctx) {
				break
			}
			err = fmt.Errorf("%s does not answer", image)
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
	}
	sql := func(script string) string {
		t.Helper()
		out, err := target.sql(test.ctx, script)
		if err != nil {
			t.Fatalf("%s: %v", script, err)
		}
		return out
	}
	rows := func() string { return sql(`SELECT COALESCE(GROUP_CONCAT(body ORDER BY id), '') FROM notes;`) }
	asApp := exec.Command("docker", "exec", "-i", target.container, "sh", "-c", `exec $(command -v mariadb || command -v mysql) -uapp -papppass shop`)
	asApp.Stdin = strings.NewReader(`CREATE TABLE notes (id INT AUTO_INCREMENT PRIMARY KEY, body TEXT, picture BLOB);
CREATE TABLE log (id INT AUTO_INCREMENT PRIMARY KEY, body TEXT);
CREATE VIEW short AS SELECT id FROM notes;
INSERT INTO notes (body, picture) VALUES ('a', UNHEX('00FF27225C0A')), ('b', NULL);`)
	if out, err := asApp.CombinedOutput(); err != nil {
		t.Fatalf("the app's tables: %v %s", err, out)
	}
	sql("CREATE DEFINER = 'app'@'%' TRIGGER noted AFTER INSERT ON notes FOR EACH ROW INSERT INTO log (body) VALUES (NEW.body);")
	sql(`INSERT INTO log (body) VALUES ('a'), ('b');`)
	sql(`CREATE PROCEDURE count_notes() SELECT COUNT(*) FROM notes;`)
	sql(`CREATE EVENT tidy ON SCHEDULE EVERY 1 DAY DO DELETE FROM log WHERE id < 0;`)
	if size, err := engine.Size(test.ctx); err != nil || size == 0 {
		t.Fatalf("the size: %d %v", size, err)
	}

	// The first copy of the day is checked before it is uploaded.
	test.b.tick(test.ctx)
	first := test.copies()[0]
	test.b.saveStates()
	if status := newBackups(test.a).status(test.ctx, app, database, false); status.CheckError != "" || status.CheckedCopy != first.ID {
		t.Fatalf("the first copy was not checked before its upload: %+v", status)
	}
	sql(`INSERT INTO notes (body) VALUES ('c');`)
	second := test.copyNow()
	test.encryptedForBoth(second)

	// The copy in the bucket is a gzipped dump with everything in it.
	from := mustCopy(t, test, second.ID)
	if from.size == 0 {
		t.Fatal("the copy carries no note of its size")
	}
	unzipped, err := gzip.NewReader(from.copy)
	if err != nil {
		t.Fatal(err)
	}
	dump, err := io.ReadAll(unzipped)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"CREATE TABLE `notes`", "count_notes", "noted", "tidy", "short"} {
		if !bytes.Contains(dump, []byte(part)) {
			t.Fatalf("the copy has no %s", part)
		}
	}

	if state := test.restore(protocol.BackupRestore{Copy: first.ID}); state.State != protocol.RestoreDone {
		t.Fatalf("restore to the first copy: %+v", state)
	}
	if got := rows(); got != "a,b" || !test.running() {
		t.Fatalf("after the restore: %q, running %v", got, test.running())
	}
	if got := sql(`SELECT HEX(picture) FROM notes WHERE body = 'a';`); got != "00FF27225C0A" {
		t.Fatalf("the picture after the restore: %q", got)
	}
	// The routine, the event, the view and the trigger came back too.
	sql(`CALL count_notes(); SELECT * FROM short;`)
	if got := sql(`SELECT COUNT(*) FROM information_schema.events WHERE event_schema = 'shop';`); got != "1" {
		t.Fatalf("events after the restore: %s", got)
	}
	sql(`INSERT INTO notes (body) VALUES ('x'); DELETE FROM notes WHERE body = 'x';`)
	if got := sql(`SELECT GROUP_CONCAT(body ORDER BY id) FROM log;`); got != "a,b,x" {
		t.Fatalf("the trigger after the restore wrote %q", got)
	}
	undos, _ := engine.Undos(test.ctx)
	if len(undos) != 1 || undos[0].Size == 0 {
		t.Fatalf("undos %+v", undos)
	}
	if state := test.restore(protocol.BackupRestore{Undo: undos[0].ID}); state.State != protocol.RestoreDone {
		t.Fatalf("undo: %+v", state)
	}
	if got := rows(); got != "a,b,c" {
		t.Fatalf("after the undo: %q", got)
	}
	// The undo it went back to is used up; the restore left a new one.
	if undos, _ := engine.Undos(test.ctx); len(undos) != 1 {
		t.Fatalf("undos after the undo: %+v", undos)
	}

	// The daily check passes a good copy and leaves no container.
	if why := test.check(); why != "" {
		t.Fatalf("the check failed: %s", why)
	}
	if out, _ := exec.Command("docker", "ps", "-a", "-q", "--filter", "name="+tempName("check-")).Output(); len(bytes.TrimSpace(out)) != 0 {
		t.Fatalf("the check left its container: %s", out)
	}

	// A copy that does not load changes nothing: the undo goes back in.
	test.putBroken()
	broken := test.copies()[0]
	if state := test.restore(protocol.BackupRestore{Copy: broken.ID}); state.State != protocol.RestoreFailed || !strings.Contains(state.Error, "as it was before") {
		t.Fatalf("restore to a broken copy: %+v", state)
	}
	if got := rows(); got != "a,b,c" || !test.running() {
		t.Fatalf("after the restore to a broken copy: %q, running %v", got, test.running())
	}

	// A restore that was cut off, before anything changed and halfway
	// through the load: the data is as it was before, and the app runs.
	for _, step := range []string{"before", "halfway"} {
		if _, err := test.b.apps.stopServices(test.ctx, app, test.b.otherServices(app, "db")...); err != nil {
			t.Fatal(err)
		}
		mark := test.killed()
		if step == "halfway" {
			if err := engine.keep(test.ctx, target, mark.Undo); err != nil {
				t.Fatal(err)
			}
			sql(`DROP TABLE log; DELETE FROM notes;`)
		}
		test.b.recover(test.ctx)
		state := test.stateOf(mark.ID)
		if state.State != protocol.RestoreFailed || !strings.Contains(state.Error, "as it was before") {
			t.Fatalf("cut off %s: %+v", step, state)
		}
		if got := rows(); got != "a,b,c" || !test.running() {
			t.Fatalf("cut off %s: %q, running %v", step, got, test.running())
		}
	}
	if undos, _ := engine.Undos(test.ctx); len(undos) != 1 {
		t.Fatalf("a restore that was cut off left its undo: %+v", undos)
	}

	// The broken copy is the newest now: it must not pass the check.
	if why := test.check(); !strings.Contains(why, "did not restore") {
		t.Fatalf("a broken copy passed the check: %q", why)
	}
	if listed, err := test.listed(); err != nil || len(listed) != 1 || listed[0].Database != "mysql-db" || listed[0].Kind != "mysql" {
		t.Fatalf("listed %+v %v", listed, err)
	}
}
