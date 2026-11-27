package cli

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/axadrn/moin/protocol"
)

// encryptedCopy is body as a copy lies in the bucket: gzipped for SQLite,
// and encrypted for key.
func encryptedCopy(t *testing.T, body string, zipped bool, key *age.X25519Identity) []byte {
	t.Helper()
	var copy bytes.Buffer
	encrypted, err := age.Encrypt(&copy, key.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if zipped {
		zip := gzip.NewWriter(encrypted)
		zip.Write([]byte(body))
		zip.Close()
	} else {
		encrypted.Write([]byte(body))
	}
	encrypted.Close()
	return copy.Bytes()
}

// Confirming your key opens a copy on your laptop: it passes only with
// the key the copy was made for, on a copy that is whole and a database.
func TestOpenCopy(t *testing.T) {
	yours, _ := age.GenerateX25519Identity()
	other, _ := age.GenerateX25519Identity()
	rows := strings.Repeat("rows ", 20000)
	sqlite := encryptedCopy(t, sqliteStart+rows, true, yours)
	postgres := encryptedCopy(t, postgresStart+rows, false, yours)
	if err := openCopy(bytes.NewReader(sqlite), false, yours); err != nil {
		t.Errorf("a SQLite copy: %v", err)
	}
	if err := openCopy(bytes.NewReader(postgres), true, yours); err != nil {
		t.Errorf("a Postgres copy: %v", err)
	}
	if err := openCopy(bytes.NewReader(sqlite), false, other); err == nil || !strings.Contains(err.Error(), "the key does not open this copy") {
		t.Errorf("another key: %v", err)
	}
	if err := openCopy(bytes.NewReader(sqlite[:len(sqlite)/2]), false, yours); err == nil {
		t.Error("half a copy passed")
	}
	if err := openCopy(bytes.NewReader(postgres[:len(postgres)-1]), true, yours); err == nil {
		t.Error("a copy cut short by one byte passed")
	}
	if err := openCopy(bytes.NewReader(encryptedCopy(t, "an error page", false, yours)), true, yours); err == nil {
		t.Error("something that is no database passed")
	}
}

// A check that failed still names the last copy that restored.
func TestCheckLineNamesTheProvenCopy(t *testing.T) {
	now := time.Now()
	checked := now.Add(-time.Hour)
	line := checkLine(protocol.DatabaseBackup{CheckedAt: &checked, CheckError: "the copy B did not restore: broken", CheckedCopy: "A"}, now)
	if !strings.Contains(line, "the copy B did not restore") || !strings.Contains(line, "The last copy that restored fine: A") {
		t.Errorf("line = %q", line)
	}
	line = checkLine(protocol.DatabaseBackup{CheckedAt: &checked, CheckError: "could not read the copy B"}, now)
	if !strings.Contains(line, "No copy restored fine yet") {
		t.Errorf("line = %q", line)
	}
}
