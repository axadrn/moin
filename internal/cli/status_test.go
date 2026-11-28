package cli

import (
	"strings"
	"testing"

	"github.com/axadrn/moin/internal/daemon"
)

func TestPrintNeedsShowsWhatTheServerSaid(t *testing.T) {
	rows := []serverRow{{server: "prod", status: daemon.Status{
		PublicSSH: true,
		Watchers:  []string{"https://watcher.example.com"},
		Needs: []daemon.Need{
			{Text: "shop: postgres has no backup yet.", Command: "moin bucket set prod"},
			{Text: "shop: the restore check failed. The last proven copy is from November 26.", Command: "moin backups shop"},
		},
	}}}
	var out strings.Builder
	printNeeds(terminal(&out), rows)
	for _, want := range []string{
		"! prod: port 22 is open to the internet.\n  `moin secure prod`",
		"! shop: postgres has no backup yet.\n  `moin bucket set prod`",
		"The last proven copy is from November 26.\n  `moin backups shop`",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("printNeeds wrote\n%s\nwant %q", out.String(), want)
		}
	}
}

// A catalog app shows which app of the catalog it runs, at which version.
func TestPrintAppsShowsTheCatalogVersion(t *testing.T) {
	var out strings.Builder
	printApps(&out, []serverRow{{server: "prod", status: daemon.Status{Apps: []daemon.AppStatus{
		{Name: "stats", Release: 3, Running: true, Catalog: "umami", Version: "latest"},
		{Name: "shop", Release: 7, Running: true},
	}}}})
	if !strings.Contains(out.String(), "umami latest") || strings.Count(out.String(), "latest") != 1 {
		t.Errorf("apps:\n%s", out.String())
	}
}
