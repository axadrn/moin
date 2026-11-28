package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/axadrn/moin/protocol"
)

// A downloaded copy is the plain database: SQLite unzipped, a dump as it
// is. Another key or half a copy writes nothing usable and fails.
func TestWritePlain(t *testing.T) {
	yours, _ := age.GenerateX25519Identity()
	other, _ := age.GenerateX25519Identity()
	rows := strings.Repeat("rows ", 20000)
	for _, c := range []struct {
		kind   string
		zipped bool
		body   string
	}{
		{"", true, sqliteStart + rows},
		{protocol.DatabasePostgres, false, postgresStart + rows},
	} {
		var out bytes.Buffer
		size, err := writePlain(bytes.NewReader(encryptedCopy(t, c.body, c.zipped, yours)), c.kind, []age.Identity{yours}, &out)
		if err != nil || out.String() != c.body || size != int64(len(c.body)) {
			t.Errorf("kind %q: %d bytes, %v", c.kind, size, err)
		}
	}
	sqlite := encryptedCopy(t, sqliteStart+rows, true, yours)
	if _, err := writePlain(bytes.NewReader(sqlite), "", []age.Identity{other}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "does not open") {
		t.Errorf("another key: %v", err)
	}
	if _, err := writePlain(bytes.NewReader(sqlite[:len(sqlite)/2]), "", []age.Identity{yours}, &bytes.Buffer{}); err == nil {
		t.Error("half a copy passed")
	}
}

// The newest copy downloads by default, --to only one the bucket has, and
// a database without a copy gets a fresh one.
func TestPickDownload(t *testing.T) {
	database := protocol.DatabaseBackup{Database: "data/app.db", Copies: []protocol.Copy{{ID: "20261127T140000Z-2"}, {ID: "20261126T140000Z-1"}}}
	if copy, err := pickDownload("my-app", database, ""); err != nil || copy != "20261127T140000Z-2" {
		t.Errorf("the newest: %q, %v", copy, err)
	}
	if copy, err := pickDownload("my-app", database, "20261126T140000Z-1"); err != nil || copy != "20261126T140000Z-1" {
		t.Errorf("an older one: %q, %v", copy, err)
	}
	if _, err := pickDownload("my-app", database, "nope"); err == nil || !strings.Contains(err.Error(), "backups my-app") {
		t.Errorf("a copy the bucket does not have: %v", err)
	}
	if copy, err := pickDownload("my-app", protocol.DatabaseBackup{Database: "data/app.db"}, ""); err != nil || copy != "" {
		t.Errorf("no copy yet: %q, %v", copy, err)
	}
}

// The file is named after the app, the copy and what it holds.
func TestDownloadName(t *testing.T) {
	for _, c := range []struct {
		database, kind string
		several        bool
		want           string
	}{
		{"/data/app.db", "", false, "my-app-c1.db"},
		{"/data/app.db", "", true, "my-app-app-c1.db"},
		{"postgres-db", protocol.DatabasePostgres, false, "my-app-c1.dump"},
		{"postgres-db/other", protocol.DatabasePostgres, true, "my-app-postgres-db-other-c1.dump"},
		{"mysql-db", protocol.DatabaseMySQL, false, "my-app-c1.sql.gz"},
		{"mongo-db", protocol.DatabaseMongo, false, "my-app-c1.archive.gz"},
	} {
		if got := downloadName("my-app", c.database, c.several, "c1", c.kind); got != c.want {
			t.Errorf("%s: %s, want %s", c.database, got, c.want)
		}
	}
	if got := downloadName("my-app", "/data/app.db", false, "", ""); !strings.HasPrefix(got, "my-app-"+time.Now().UTC().Format("20060102")) {
		t.Errorf("a fresh copy: %s", got)
	}
}
