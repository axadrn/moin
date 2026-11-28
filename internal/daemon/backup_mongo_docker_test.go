//go:build docker

package daemon

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// The promises of backups against a real MongoDB: a copy, a restore, its
// undo, a restore that was cut off, the daily check, and a broken copy
// that must not pass it. Run like the other backup tests.
func TestBackupMongo(t *testing.T) {
	for _, image := range []string{"mongo:7", "mongo:8"} {
		t.Run(image, func(t *testing.T) { testBackupMongo(t, image) })
	}
}

func testBackupMongo(t *testing.T, image string) {
	app := fmt.Sprintf("bk-mongo-%d", time.Now().UnixNano()%1e6)
	compose := map[string]any{"services": map[string]any{
		"web": map[string]any{"image": "alpine:latest", "command": []string{"sleep", "infinity"}, "init": true},
		"db":  map[string]any{"image": image, "environment": map[string]string{"MONGO_INITDB_ROOT_USERNAME": "root", "MONGO_INITDB_ROOT_PASSWORD": "s3'cret\" x"}},
	}}
	database := protocol.Database{Path: protocol.ServiceKey(protocol.DatabaseMongo, "db"), Kind: protocol.DatabaseMongo, Service: "db"}
	test := newBackupTest(t, app, compose, database)
	engine := newMongoEngine(test.b, app, database)
	target, err := engine.target(test.ctx)
	if err == nil {
		err = waitFor(test.ctx, target.answers)
	}
	if err != nil {
		t.Fatal(err)
	}
	js := func(script string) string {
		t.Helper()
		out, err := target.js(test.ctx, script)
		if err != nil {
			t.Fatalf("%s: %v", script, err)
		}
		return out
	}
	rows := func() string {
		return js(`print(db.getSiblingDB("shop").notes.find().sort({_id: 1}).toArray().map((note) => note.body).join(","));`)
	}
	js(`const shop = db.getSiblingDB("shop");
shop.notes.insertMany([{_id: 1, body: "a", picture: BinData(0, "AP8nIlwK")}, {_id: 2, body: "b"}]);
shop.notes.createIndex({body: 1}, {unique: true});
db.getSiblingDB("wiki").pages.insertOne({title: "home"});`)
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
	js(`db.getSiblingDB("shop").notes.insertOne({_id: 3, body: "c"}); db.getSiblingDB("later").things.insertOne({});`)
	second := test.copyNow()
	test.encryptedForBoth(second)

	// The copy in the bucket is a gzipped archive of mongodump.
	from := mustCopy(t, test, second.ID)
	if from.size == 0 {
		t.Fatal("the copy carries no note of its size")
	}
	unzipped, err := gzip.NewReader(from.copy)
	if err != nil {
		t.Fatal(err)
	}
	start := make([]byte, 4)
	if _, err := unzipped.Read(start); err != nil || !bytes.Equal(start, []byte{0x6d, 0xe2, 0x99, 0x81}) {
		t.Fatalf("the copy is no archive of mongodump: %x %v", start, err)
	}

	if state := test.restore(protocol.BackupRestore{Copy: first.ID}); state.State != protocol.RestoreDone {
		t.Fatalf("restore to the first copy: %+v", state)
	}
	if got := rows(); got != "a,b" || !test.running() {
		t.Fatalf("after the restore: %q, running %v", got, test.running())
	}
	// The picture, the index and the other database came back. A database
	// made after the copy is gone, and root still gets in.
	if got := js(`const shop = db.getSiblingDB("shop");
print(shop.notes.findOne({_id: 1}).picture.toString("base64"), shop.notes.getIndexes().length, db.getSiblingDB("wiki").pages.countDocuments(), databases().map((d) => d.name).join(","));`); got != "AP8nIlwK 2 1 shop,wiki" {
		t.Fatalf("after the restore: %q", got)
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
			if err := engine.keep(test.ctx, mark.Undo); err != nil {
				t.Fatal(err)
			}
			js(`db.getSiblingDB("wiki").dropDatabase(); db.getSiblingDB("shop").notes.deleteMany({});`)
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
	if listed, err := test.listed(); err != nil || len(listed) != 1 || listed[0].Database != "mongo-db" || listed[0].Kind != "mongo" {
		t.Fatalf("listed %+v %v", listed, err)
	}
}
