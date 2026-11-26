package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/dbbackup"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/testutil"
)

const testReleaseBody = `{"tag_name":"v0.3.0","html_url":"https://github.com/axadrn/moin/releases/tag/v0.3.0","published_at":"2026-11-02T10:00:00Z",
"body":"**In short:** Faster deploys.\n\n### New\n\n- **Deploys**: faster.\n"}`

// TestUpdateCheck reads the newest release from GitHub, asks again with
// its ETag, and keeps the release on a 304.
func TestUpdateCheck(t *testing.T) {
	var requests atomic.Int32
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("If-None-Match") == `"abc"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"abc"`)
		w.Write([]byte(testReleaseBody))
	}))
	defer github.Close()
	updates := NewUpdateService("v0.2.0", github.URL, true, true, []string{"ada@example.com"})
	for range 2 {
		if err := updates.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	info := updates.Info(nil)
	if info.Latest != "v0.3.0" || !info.UpdateAvailable || info.Summary != "Faster deploys." ||
		info.NotesURL != "https://github.com/axadrn/moin/releases/tag/v0.3.0" || info.PublishedAt == nil || !strings.Contains(info.Notes, "### New") {
		t.Fatalf("info = %+v", info)
	}
	if info.Admin {
		t.Fatalf("a visitor is an admin: %+v", info)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d", requests.Load())
	}

	// A development build never reports an update.
	dev := NewUpdateService("dev", github.URL, true, true, nil)
	if err := dev.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if info := dev.Info(nil); info.UpdateAvailable || info.Latest != "v0.3.0" {
		t.Fatalf("dev info = %+v", info)
	}
}

// TestHostedShowsNoUpdate: the hosted hub updates with every
// release; it neither asks GitHub nor offers an update.
func TestHostedShowsNoUpdate(t *testing.T) {
	var asked atomic.Bool
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Store(true)
		w.Write([]byte(testReleaseBody))
	}))
	defer github.Close()
	database := testutil.NewDatabase(t)
	fixture := testutil.SeedUser(t, database, testutil.UserOptions{ID: "ada", Email: "ada@example.com", Verified: true})
	hosted := NewUpdateService("v0.2.0", github.URL, false, true, []string{"ada@example.com"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hosted.Run(ctx)
	info := hosted.Info(fixture.User)
	if asked.Load() || info.SelfHosted || info.Latest != "" || info.UpdateAvailable || info.Admin {
		t.Fatalf("hosted info = %+v, asked %v", info, asked.Load())
	}
}

// TestDaemonBehind: a daemon built from a commit, not a release, updates to
// the release; a development hub updates nobody.
func TestDaemonBehind(t *testing.T) {
	for _, test := range []struct {
		release, daemon string
		behind          bool
	}{
		{"v1.0.0-beta.6", "v1.0.0-beta.5", true},
		{"v1.0.0-beta.6", "v1.0.0-beta.6", false},
		{"v1.0.0-beta.6", "v1.0.0-beta.7", false},
		{"v1.0.0-beta.6", "b4be272", true},
		{"v1.0.0-beta.6", "dev", true},
		{"v1.0.0-beta.6", "", true},
		{"dev", "b4be272", false},
		{"b4be272", "v1.0.0", false},
	} {
		if got := DaemonBehind(test.release, test.daemon); got != test.behind {
			t.Errorf("DaemonBehind(%q, %q) = %v, want %v", test.release, test.daemon, got, test.behind)
		}
	}
}

func TestHubBackupGoesToTheAdminsOfEveryHub(t *testing.T) {
	now := time.Now()
	verified := &model.User{Email: "ada@example.com", EmailVerifiedAt: &now}
	member := &model.User{Email: "grace@example.com", EmailVerifiedAt: &now}
	hosted := NewUpdateService("v0.2.0", "", false, false, []string{"ada@example.com"})
	if backup := hosted.Info(verified).HubBackup; backup == nil || backup.State != model.HubBackupOff {
		t.Fatalf("hub backup without one = %+v, want off", backup)
	}
	if hosted.Info(member).HubBackup != nil {
		t.Fatal("a member who does not run the hub sees its backup")
	}
	hosted.UseHubBackup(&dbbackup.Backup{Bucket: "acme-hub", Started: now})
	if backup := hosted.HubBackup(); backup.State != model.HubBackupOn || backup.Bucket != "acme-hub" {
		t.Fatalf("hub backup just started = %+v, want on", backup)
	}
	hosted.UseHubBackup(&dbbackup.Backup{Bucket: "acme-hub", Started: now.Add(-model.HubBackupLateAfter - time.Second)})
	if backup := hosted.HubBackup(); backup.State != model.HubBackupLate {
		t.Fatalf("hub backup without a copy for long = %+v, want late", backup)
	}
}
