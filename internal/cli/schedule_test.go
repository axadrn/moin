package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/axadrn/moin/internal/daemon"
)

// `moin backups` changes the schedule of an app, or with -s and no app
// the server's, on the server's side of `here schedule`.
func TestBackupsSchedule(t *testing.T) {
	runs := fakeSSH(t, `{"app": "shop", "every": "15m", "keep": "30d", "own": true}`)
	var out bytes.Buffer
	if err := backups(context.Background(), []string{"shop", "--keep", "30d", "-s", "prod"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "shop: a copy every 15m, each kept 30d") {
		t.Errorf("said %q", out.String())
	}
	runs2 := fakeSSH(t, `{"every": "6h", "keep": "7d", "own": true}`)
	out.Reset()
	if err := backups(context.Background(), []string{"--every", "6h", "-s", "prod"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "prod takes a copy every 6h") {
		t.Errorf("said %q", out.String())
	}
	app, _ := os.ReadFile(runs)
	server, _ := os.ReadFile(runs2)
	if !strings.Contains(string(app), "'here' 'schedule' '--every' '' '--keep' '30d' 'shop'") {
		t.Errorf("for the app ssh ran %q", app)
	}
	if !strings.Contains(string(server), "'here' 'schedule' '--every' '6h' '--keep' ''") || strings.Contains(string(server), "shop") {
		t.Errorf("for the server ssh ran %q", server)
	}
}

// The schedule line says whose schedule an app runs on.
func TestScheduleLine(t *testing.T) {
	if got := scheduleLine(daemon.BackupSchedule{Every: "1h", Keep: "7d"}); !strings.Contains(got, "A copy every 1h, each kept 7d.") || !strings.Contains(got, "The server's") {
		t.Errorf("the server's: %q", got)
	}
	if got := scheduleLine(daemon.BackupSchedule{Every: "15m", Keep: "30d", Own: true}); !strings.Contains(got, "Its own") {
		t.Errorf("its own: %q", got)
	}
}
