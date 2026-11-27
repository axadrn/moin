package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
