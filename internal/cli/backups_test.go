package cli

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/axadrn/moin/internal/alert"
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
	if err := openCopy(bytes.NewReader(sqlite), "", yours); err != nil {
		t.Errorf("a SQLite copy: %v", err)
	}
	if err := openCopy(bytes.NewReader(postgres), "postgres", yours); err != nil {
		t.Errorf("a Postgres copy: %v", err)
	}
	for _, start := range []string{"-- MySQL dump 10.13\n", "/*M!999999\\- enable the sandbox mode */\n"} {
		if err := openCopy(bytes.NewReader(encryptedCopy(t, start+rows, true, yours)), "mysql", yours); err != nil {
			t.Errorf("a MySQL copy that starts %q: %v", start, err)
		}
	}
	if err := openCopy(bytes.NewReader(encryptedCopy(t, "\x6d\xe2\x99\x81"+rows, true, yours)), "mongo", yours); err != nil {
		t.Errorf("a MongoDB copy: %v", err)
	}
	if err := openCopy(bytes.NewReader(encryptedCopy(t, "PK\x03\x04"+rows, false, yours)), "clickhouse", yours); err != nil {
		t.Errorf("a ClickHouse copy: %v", err)
	}
	if err := openCopy(bytes.NewReader(encryptedCopy(t, "REDIS0012"+rows, true, yours)), "redis", yours); err != nil {
		t.Errorf("a Redis copy: %v", err)
	}
	if err := openCopy(bytes.NewReader(encryptedCopy(t, "short", true, yours)), "mongo", yours); err == nil {
		t.Error("a few bytes that are no database passed")
	}
	if err := openCopy(bytes.NewReader(encryptedCopy(t, "an error page that is long enough", true, yours)), "mysql", yours); err == nil {
		t.Error("gzipped text that is no database passed")
	}
	if err := openCopy(bytes.NewReader(sqlite), "", other); err == nil || !strings.Contains(err.Error(), "the key does not open this copy") {
		t.Errorf("another key: %v", err)
	}
	if err := openCopy(bytes.NewReader(sqlite[:len(sqlite)/2]), "", yours); err == nil {
		t.Error("half a copy passed")
	}
	if err := openCopy(bytes.NewReader(postgres[:len(postgres)-1]), "postgres", yours); err == nil {
		t.Error("a copy cut short by one byte passed")
	}
	if err := openCopy(bytes.NewReader(encryptedCopy(t, "an error page", false, yours)), "postgres", yours); err == nil {
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

// TestAddEmailAlert: the password of an email alert comes from stdin and
// goes to the server on ssh's stdin. It is never a flag, and never an
// argument of ssh, which other users of a machine can see.
func TestAddEmailAlert(t *testing.T) {
	dir := t.TempDir()
	sent, runs := filepath.Join(dir, "sent"), filepath.Join(dir, "runs")
	fake := "#!/bin/sh\necho \"$@\" >> " + runs + "\nread -r line\nprintf '%s' \"$line\" > " + sent + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	args := []string{"prod", "email", "--to", "ada@example.com", "--smtp", "smtp.example.com:587", "--user", "alerts@example.com"}

	if err := addAlert(context.Background(), args, strings.NewReader("secret-password\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	var got alert.Destination
	data, _ := os.ReadFile(sent)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("the server got %q: %v", data, err)
	}
	want := alert.Destination{Kind: "email", To: "ada@example.com", SMTP: "smtp.example.com:587", User: "alerts@example.com", Password: "secret-password"}
	if got != want {
		t.Errorf("the server got %+v, want %+v", got, want)
	}
	if ran, _ := os.ReadFile(runs); strings.Contains(string(ran), "secret-password") || !strings.Contains(string(ran), "'alerts' 'add'") {
		t.Errorf("ssh ran with %q", ran)
	}

	if err := addAlert(context.Background(), args, strings.NewReader(""), io.Discard); err == nil {
		t.Error("an email alert without a password: want an error")
	}
	if err := addAlert(context.Background(), append(args, "--password", "secret-password"), strings.NewReader(""), io.Discard); err == nil {
		t.Error("--password is a flag: want an error")
	}
}

// A webhook gets a secret that signs its posts. The server gets it on
// stdin, never in ssh's arguments, and the CLI shows it once.
func TestAddWebhookAlertSecret(t *testing.T) {
	dir := t.TempDir()
	sent, runs := filepath.Join(dir, "sent"), filepath.Join(dir, "runs")
	fake := "#!/bin/sh\necho \"$@\" >> " + runs + "\nread -r line\nprintf '%s' \"$line\" > " + sent + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var out strings.Builder
	if err := addAlert(context.Background(), []string{"prod", "webhook", "--url", "https://hooks.example.com/moin"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	var got alert.Destination
	data, _ := os.ReadFile(sent)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("the server got %q: %v", data, err)
	}
	if got.Kind != "webhook" || len(got.Secret) < 20 || !strings.Contains(out.String(), got.Secret) {
		t.Errorf("the server got %+v, the CLI said %q", got, out.String())
	}
	if ran, _ := os.ReadFile(runs); strings.Contains(string(ran), got.Secret) {
		t.Errorf("ssh ran with the secret: %q", ran)
	}
}

// A Redis that is only a cache is listed with its note and nothing else,
// and is no database to pick for a restore.
func TestCacheIsListedNotPicked(t *testing.T) {
	cache := databaseBackups{DatabaseBackup: protocol.DatabaseBackup{Database: "redis-cache", Kind: "redis", Note: "cache, not backed up"}}
	var out strings.Builder
	printBackup(&out, cache, time.Now())
	if got := out.String(); !strings.Contains(got, "redis-cache") || !strings.Contains(got, "cache, not backed up") || strings.Contains(got, "check") {
		t.Errorf("the cache prints as\n%s", got)
	}
	database := databaseBackups{DatabaseBackup: protocol.DatabaseBackup{Database: "postgres-db"}}
	picked, err := pickDatabase("shop", "", []databaseBackups{database, cache}, strings.NewReader(""), &out)
	if err != nil || picked.Database != "postgres-db" {
		t.Errorf("picked %q, %v", picked.Database, err)
	}
}
