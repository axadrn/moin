package daemon

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestSQLiteLinks: an app can leave links in its data folder. The daemon,
// which runs as root, never reads or writes through one out of the folder.
func TestSQLiteLinks(t *testing.T) {
	folder := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "other.db")
	if err := os.WriteFile(secret, []byte("someone else's"), 0o600); err != nil {
		t.Fatal(err)
	}

	// app.db swapped for a link to another file.
	if err := os.Symlink(secret, filepath.Join(folder, "app.db")); err != nil {
		t.Fatal(err)
	}
	linked := protocol.Database{Path: "/data/app.db", HostPath: filepath.Join(folder, "app.db"), HostFolder: folder}
	if err := plainDatabase(linked); err == nil || !strings.Contains(err.Error(), "not a plain file") {
		t.Fatalf("opened a link: %v", err)
	}

	// A folder on the way swapped for a link out of the data folder.
	if err := os.Symlink(outside, filepath.Join(folder, "sub")); err != nil {
		t.Fatal(err)
	}
	through := protocol.Database{Path: "/data/sub/other.db", HostPath: filepath.Join(folder, "sub", "other.db"), HostFolder: folder}
	if err := plainDatabase(through); err == nil {
		t.Fatal("followed a link out of the folder")
	}
	root, name, err := databaseFolder(through)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var zipped bytes.Buffer
	writer := gzip.NewWriter(&zipped)
	writer.Write([]byte("bytes from a stranger"))
	writer.Close()
	engine := &sqliteEngine{database: through}
	err = engine.buildChecked(context.Background(), root, source{copy: &zipped, size: 1}, name+".moin-restore")
	if err == nil {
		t.Fatal("built a restore through a link out of the folder")
	}
	if _, err := os.Stat(filepath.Join(outside, "other.db.moin-restore")); err == nil {
		t.Fatal("wrote outside the data folder")
	}
	if body, _ := os.ReadFile(secret); string(body) != "someone else's" {
		t.Fatalf("changed a file outside the folder: %q", body)
	}
}
