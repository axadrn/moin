package cli

import (
	"bytes"
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
}

// `moin cloud copies` lists what moin cloud answers, and restore picks a
// copy by its ID, or asks for --to without a terminal.
func TestCloudCopies(t *testing.T) {
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.URL.Path != "/api/cloud/copies" {
			http.NotFound(w, r)
			return
		}
		list := []cloudCopy{{ID: "NEW", Server: "prod", App: "shop", Database: "/data/app.db", MadeAt: time.Now(), Size: 2048}, {ID: "OLD", Server: "prod", App: "shop", Database: "postgres-db", MadeAt: time.Now().Add(-time.Hour), Size: 1024}}
		if r.URL.Query().Get("server") != "" && r.URL.Query().Get("server") != "prod" {
			list = nil
		}
		json.NewEncoder(w).Encode(list)
	}))
	defer cloud.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MOIN_CLOUD_URL", cloud.URL)
	if err := saveCloudSession(cloudSession{URL: cloudURL(), Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cloudCopies(context.Background(), []string{"prod", "shop"}, &out); err != nil || !strings.Contains(out.String(), "NEW") || !strings.Contains(out.String(), "postgres-db") {
		t.Fatalf("copies: %v\n%s", err, out.String())
	}
	out.Reset()
	if err := cloudCopies(context.Background(), []string{"web-1"}, &out); err != nil || !strings.Contains(out.String(), "No copies of web-1") {
		t.Fatalf("no copies: %v\n%s", err, out.String())
	}
	session, _ := loadCloudSession()
	if copy, err := pickCloudCopy(context.Background(), session, "shop", "postgres-db", "OLD", strings.NewReader(""), io.Discard); err != nil || copy.Database != "postgres-db" {
		t.Errorf("--to OLD: %+v, %v", copy, err)
	}
	if _, err := pickCloudCopy(context.Background(), session, "shop", "postgres-db", "NEW", strings.NewReader(""), io.Discard); err == nil {
		t.Error("a copy of another database was picked")
	}
	if _, err := pickCloudCopy(context.Background(), session, "shop", "", "", strings.NewReader(""), io.Discard); err == nil || !strings.Contains(err.Error(), "--to") {
		t.Errorf("without a terminal: %v", err)
	}
}
