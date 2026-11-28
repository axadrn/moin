package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// `moin watcher logout` ends the session at moin cloud and deletes it on
// the laptop, also when moin cloud does not answer.
func TestCloudLogout(t *testing.T) {
	ended := ""
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/cli/logout" {
			http.NotFound(w, r)
			return
		}
		ended = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer cloud.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MOIN_CLOUD_URL", cloud.URL)
	if err := saveCloudSession(cloudSession{URL: cloudURL(), Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := watcherCommand(context.Background(), []string{"logout"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if ended != "Bearer secret" {
		t.Errorf("moin cloud got %q", ended)
	}
	if _, err := loadCloudSession(); !errors.Is(err, errNotSignedIn) {
		t.Errorf("the session is still on the laptop: %v", err)
	}
	out.Reset()
	if err := watcherCommand(context.Background(), []string{"logout"}, nil, &out); err != nil || !strings.Contains(out.String(), "not signed in") {
		t.Errorf("twice: %q, %v", out.String(), err)
	}

	cloud.Close()
	if err := saveCloudSession(cloudSession{URL: cloudURL(), Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := watcherCommand(context.Background(), []string{"logout"}, nil, &out); err != nil || !strings.Contains(out.String(), "/cloud/settings") {
		t.Errorf("moin cloud away: %q, %v", out.String(), err)
	}
	if _, err := loadCloudSession(); !errors.Is(err, errNotSignedIn) {
		t.Errorf("moin cloud away, the session is still on the laptop: %v", err)
	}
}
