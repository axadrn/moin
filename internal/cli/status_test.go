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
		"prod: port 22 open to the internet",
		"`moin secure prod`",
		"shop: postgres has no backup yet.  `moin bucket set prod`",
		"The last proven copy is from November 26.  `moin backups shop`",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("printNeeds wrote\n%s\nwant %q", out.String(), want)
		}
	}
}
