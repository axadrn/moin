package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// The session is only for you to read, and only for the moin cloud it
// came from.
func TestCloudSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MOIN_CLOUD_URL", "http://localhost:8090/")
	if _, err := loadCloudSession(); !errors.Is(err, errNotSignedIn) {
		t.Fatalf("before login: %v", err)
	}
	if err := saveCloudSession(cloudSession{URL: cloudURL(), Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	file, _ := cloudSessionFile()
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0o600 || filepath.Base(file) != "cloud.json" {
		t.Fatalf("session file: %v %v", info.Mode(), err)
	}
	if session, err := loadCloudSession(); err != nil || session.Token != "secret" || session.URL != "http://localhost:8090" {
		t.Fatalf("session = %+v, %v", session, err)
	}
	t.Setenv("MOIN_CLOUD_URL", "")
	if _, err := loadCloudSession(); !errors.Is(err, errNotSignedIn) {
		t.Errorf("a session of another moin cloud: %v", err)
	}
	if copies := inCloud(context.Background(), "prod", "", "shop", io.Discard); copies != nil {
		t.Errorf("a laptop that is not signed in lists copies of moin cloud: %+v", copies)
	}
}

// signedInCloud is a moin cloud with two copies of shop on prod, and a
// laptop signed in to it.
func signedInCloud(t *testing.T) {
	t.Helper()
	now := time.Now()
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "not signed in", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/cloud/copies" {
			http.NotFound(w, r)
			return
		}
		list := []map[string]any{
			{"id": "NEW", "server": "prod", "app": "shop", "database": "/data/app.db", "made_at": now, "size": 2048},
			{"id": "OLD", "server": "prod", "app": "shop", "database": "postgres-db", "made_at": now.Add(-time.Hour), "size": 1024},
		}
		if r.URL.Query().Get("server") != "prod" {
			list = nil
		}
		json.NewEncoder(w).Encode(list)
	}))
	t.Cleanup(cloud.Close)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MOIN_CLOUD_URL", cloud.URL)
	if err := saveCloudSession(cloudSession{URL: cloudURL(), Token: "secret"}); err != nil {
		t.Fatal(err)
	}
}

// The copies in moin cloud are listed with where they are, and --to finds
// one by its ID, of the database named, or else leaves it to the bucket.
func TestCopiesInCloud(t *testing.T) {
	signedInCloud(t)
	ctx := context.Background()
	copies := inCloud(ctx, "prod", "", "shop", io.Discard)
	if len(copies) != 2 || copies[0].Where != cloudName || copies[0].Database != "/data/app.db" {
		t.Fatalf("copies = %+v", copies)
	}
	if copies := inCloud(ctx, "web-1", "prod", "shop", io.Discard); len(copies) != 2 {
		t.Errorf("--from prod: %+v", copies)
	}
	if database, copy, err := pickCopy(ctx, "prod", "shop", "", "", "OLD", strings.NewReader(""), io.Discard); err != nil || database != "postgres-db" || copy.Where != cloudName {
		t.Errorf("--to OLD: %s %+v, %v", database, copy, err)
	}
	if _, copy, err := pickCopy(ctx, "prod", "shop", "postgres-db", "", "NEW", strings.NewReader(""), io.Discard); err != nil || copy.Where != inBucket {
		t.Errorf("a copy of another database was picked: %+v, %v", copy, err)
	}
	if _, copy, err := pickCopy(ctx, "prod", "shop", "", "", "2026-11-27T14:07", strings.NewReader(""), io.Discard); err != nil || copy.Where != inBucket {
		t.Errorf("a time is the bucket's: %+v, %v", copy, err)
	}
	if _, _, err := pickCopy(ctx, "prod", "shop", "", "", "", strings.NewReader(""), io.Discard); err == nil || !strings.Contains(err.Error(), "--to") {
		t.Errorf("without a terminal: %v", err)
	}
}

// A copy in moin cloud shows in the table of its database with where it
// is, beside the copies in the bucket.
func TestPrintBackupShowsWhere(t *testing.T) {
	now := time.Now()
	database := databaseBackups{
		DatabaseBackup: protocol.DatabaseBackup{Database: "/data/app.db"},
		Copies: []storedCopy{
			{ID: "CLOUD1", Where: cloudName, At: now.Add(-time.Minute), Size: 2048},
			{ID: "BUCKET1", Where: inBucket, At: now.Add(-time.Hour), Size: 1024},
		},
	}
	var out strings.Builder
	printBackup(&out, database, now)
	for _, want := range []string{"WHERE", "CLOUD1", cloudName, "BUCKET1", inBucket} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in\n%s", want, out.String())
		}
	}
}

// A trial counts the day it is in.
func TestTrialDaysLeft(t *testing.T) {
	now := time.Now()
	ends := now.Add(36 * time.Hour)
	if days := trialDaysLeft(cloudWorkspace{Status: "trial", TrialEnds: &ends}, now); days != 2 {
		t.Errorf("36 hours = %d days", days)
	}
	if days := trialDaysLeft(cloudWorkspace{Status: "paid", TrialEnds: &ends}, now); days != 0 {
		t.Errorf("paid = %d days", days)
	}
	ended := now.Add(-time.Hour)
	if days := trialDaysLeft(cloudWorkspace{Status: "trial", TrialEnds: &ended}, now); days != 0 {
		t.Errorf("ended = %d days", days)
	}
}
