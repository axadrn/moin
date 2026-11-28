package daemon

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/axadrn/moin/protocol"
)

// Every copy is encrypted for two keys: the server's and yours. Each
// opens it alone, no other key does, and what lies in the bucket shows
// nothing of the database.
func TestEncryptForTheServerAndForYou(t *testing.T) {
	t.Setenv("MOIN_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	yours, _ := age.GenerateX25519Identity()
	stranger, _ := age.GenerateX25519Identity()
	b := newBackups(newApps(t.TempDir()))
	b.configure(Config{Name: "prod", BackupKey: &BackupKey{Public: yours.Recipient().String()}})
	secret := []byte("SQLite format 3, and the names of every customer")
	var inBucket bytes.Buffer
	encrypted, err := b.encrypt(&inBucket)
	if err != nil {
		t.Fatal(err)
	}
	encrypted.Write(secret)
	if err := encrypted.Close(); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(inBucket.Bytes(), []byte("customer")) || !bytes.HasPrefix(inBucket.Bytes(), []byte("age-encryption.org/v1")) {
		t.Fatalf("in the bucket: %q", inBucket.Bytes())
	}
	open := func(b *backups) (string, error) {
		plain, err := b.decrypt(protocol.Copy{ID: "x"}, bytes.NewReader(inBucket.Bytes()))
		if err != nil {
			return "", err
		}
		body, err := io.ReadAll(plain)
		return string(body), err
	}
	// The server opens it with its own key, as the daily check does.
	if got, err := open(b); err != nil || got != string(secret) {
		t.Fatalf("the server's key: %q %v", got, err)
	}
	// The server is gone, and its key with it: yours opens the copy.
	os.Remove(serverKeyFile())
	if _, err := open(b); !errors.Is(err, errWrongKey) {
		t.Fatalf("a server without a key: %v", err)
	}
	b.key = yours
	if got, err := open(b); err != nil || got != string(secret) {
		t.Fatalf("your key: %q %v", got, err)
	}
	b.key = stranger
	if _, err := open(b); !errors.Is(err, errWrongKey) {
		t.Fatalf("a stranger's key: %v", err)
	}
	// A copy that was cut short does not read to its end as if it were whole.
	plain, err := DecryptCopy(bytes.NewReader(inBucket.Bytes()[:inBucket.Len()-5]), yours)
	if err == nil {
		_, err = io.ReadAll(plain)
	}
	if err == nil {
		t.Fatal("a copy that was cut short read fine")
	}
	// A plain copy, from before, is read as it is.
	old, err := b.decrypt(protocol.Copy{ID: "x", Plain: true}, strings.NewReader("plain"))
	if body, _ := io.ReadAll(old); err != nil || string(body) != "plain" {
		t.Fatalf("a plain copy: %q %v", body, err)
	}
}

// Without a key of yours a copy is still encrypted, for the server alone.
func TestEncryptWithoutYourKey(t *testing.T) {
	t.Setenv("MOIN_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	b := newBackups(newApps(t.TempDir()))
	b.configure(Config{Name: "prod"})
	var inBucket bytes.Buffer
	encrypted, err := b.encrypt(&inBucket)
	if err != nil {
		t.Fatal(err)
	}
	encrypted.Write([]byte("rows"))
	encrypted.Close()
	plain, err := b.decrypt(protocol.Copy{}, &inBucket)
	if body, _ := io.ReadAll(plain); err != nil || string(body) != "rows" {
		t.Fatalf("%q %v", body, err)
	}
}

// The server's key is made once, for root only, and stays the same.
func TestServerKey(t *testing.T) {
	t.Setenv("MOIN_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	if _, err := serverKey(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a key before the first copy: %v", err)
	}
	first, err := makeServerKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := makeServerKey()
	if err != nil || first.String() != second.String() {
		t.Fatalf("a second key: %v", err)
	}
	info, err := os.Stat(serverKeyFile())
	if err != nil || info.Mode().Perm() != 0o600 || filepath.Base(serverKeyFile()) != "backup.key" {
		t.Fatalf("%s: %v %v", serverKeyFile(), info, err)
	}
	if left, _ := filepath.Glob(serverKeyFile() + ".*"); len(left) != 0 {
		t.Fatalf("left over: %v", left)
	}
	os.WriteFile(serverKeyFile(), []byte("not a key"), 0o600)
	if _, err := makeServerKey(); err == nil {
		t.Fatal("a broken key file was replaced without a word")
	}
}

// Your key: the server keeps its public half, a new one is not confirmed,
// and only the key that is there can be confirmed. `moin` says what it
// needs until then.
func TestBackupKeyHere(t *testing.T) {
	useConfig(t, "name: prod\nbucket:\n  bucket: b\n  access_key: k\n  secret_key: s\n")
	needs := func() string {
		config, _ := LoadConfig()
		said := ""
		for _, need := range backupKeyNeeds(config, nil) {
			said += need.Text + " " + need.Command
		}
		return said
	}
	if !strings.Contains(needs(), "bucket key prod") {
		t.Fatalf("without a key: %q", needs())
	}
	yours, _ := age.GenerateX25519Identity()
	public := yours.Recipient().String()
	if err := SetBackupKeyHere("age1nope"); err == nil {
		t.Fatal("kept a key that is none")
	}
	if err := SetBackupKeyHere(public); err != nil {
		t.Fatal(err)
	}
	if key, err := BackupKeyHere(); err != nil || key.Public != public || key.Confirmed || !strings.Contains(needs(), "bucket confirm prod") {
		t.Fatalf("a new key: %+v %v %q", key, err, needs())
	}
	other, _ := age.GenerateX25519Identity()
	if err := ConfirmBackupKeyHere(other.Recipient().String()); err == nil {
		t.Fatal("confirmed another key")
	}
	if err := ConfirmBackupKeyHere(public); err != nil {
		t.Fatal(err)
	}
	// Setting the same key again keeps it confirmed; a new one is not.
	SetBackupKeyHere(public)
	if key, _ := BackupKeyHere(); !key.Confirmed || needs() != "" {
		t.Fatalf("confirmed: %+v %q", key, needs())
	}
	SetBackupKeyHere(other.Recipient().String())
	if key, _ := BackupKeyHere(); key.Confirmed {
		t.Fatal("a new key is confirmed")
	}
	// A server without a bucket needs no key, until a watcher took a copy.
	if got := backupKeyNeeds(Config{Name: "prod"}, nil); got != nil {
		t.Fatalf("without a bucket: %v", got)
	}
	at := time.Now()
	copied := map[string][]protocol.DatabaseBackup{"shop": {{Database: "/data/app.db", CopiedAt: &at}}}
	if got := backupKeyNeeds(Config{Name: "prod"}, copied); len(got) != 1 || got[0].Command != "moin bucket key prod" {
		t.Fatalf("without a bucket, with a copy at a watcher: %v", got)
	}
}

// A server without a bucket proves your key with a copy made now and
// sent as it would leave the server: your key opens it.
func TestSendCopyWithoutABucket(t *testing.T) {
	journal(t)
	b, database := shopWithSQLite(t)
	yours, _ := age.GenerateX25519Identity()
	if err := SetBackupKeyHere(yours.Recipient().String()); err != nil {
		t.Fatal(err)
	}
	var sent bytes.Buffer
	if err := SendCopyHere(context.Background(), filepath.Dir(b.apps.dir), KeyCopy{App: "shop", Database: database.Path}, &sent); err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptCopy(&sent, yours)
	if err != nil {
		t.Fatal(err)
	}
	unzipped, err := gzip.NewReader(plain)
	if err != nil {
		t.Fatal(err)
	}
	file, err := io.ReadAll(unzipped)
	if err != nil || !bytes.HasPrefix(file, []byte("SQLite format 3\x00")) {
		t.Fatalf("what your key opened is no database: %v", err)
	}
}
