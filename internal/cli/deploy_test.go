package cli

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/internal/daemon"
)

func TestPrintLiveSaysWhatTheBackupsNeed(t *testing.T) {
	result := daemon.Result{App: "shop", Release: 1, Addresses: []string{"https://shop.example.com"}, Databases: []string{"postgres"}}
	for _, test := range []struct {
		bucket    bool
		want, not string
	}{
		{false, "No backup yet: `moin bucket set prod`", "See its backups"},
		{true, "See its backups: `moin backups shop`", "No backup yet"},
	} {
		var out strings.Builder
		printLive(terminal(&out), result, "prod", test.bucket)
		if !strings.Contains(out.String(), test.want) || strings.Contains(out.String(), test.not) {
			t.Errorf("bucket %v: printLive wrote\n%s\nwant %q, not %q", test.bucket, out.String(), test.want, test.not)
		}
	}
}

func TestPrintLiveWithoutADatabaseSaysNothingOfBackups(t *testing.T) {
	var out strings.Builder
	printLive(terminal(&out), daemon.Result{App: "shop", Release: 1, Addresses: []string{"https://shop.example.com"}}, "prod", false)
	if strings.Contains(out.String(), "backup") {
		t.Errorf("printLive wrote\n%s\nwant nothing about backups", out.String())
	}
}

func TestNearCommand(t *testing.T) {
	for typed, want := range map[string]string{"deplyo": "deploy", "lgos": "logs", "backpus": "backups", "restor": "restore", "zebra": ""} {
		if got := nearCommand(typed); got != want {
			t.Errorf("nearCommand(%q) = %q, want %q", typed, got, want)
		}
	}
}
