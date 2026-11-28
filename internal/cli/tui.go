package cli

import (
	"context"
	"io"

	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/tui"
)

// openTUI is `moin` alone on a terminal: the terminal UI, on the same
// functions as the commands. Without servers there is nothing to show
// yet, so it says how to start, as `moin` does.
func openTUI(ctx context.Context, stdout io.Writer) error {
	servers, err := loadServers()
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		return overview(ctx, "", nil, stdout)
	}
	return tui.Run(ctx, tui.CLI{
		Servers: servers,
		Status:  quietStatus,
		Logs: func(ctx context.Context, server, app string, out io.Writer) error {
			return appLogs(ctx, server, app, 200, true, out, out)
		},
		Backups:  appBackups,
		Releases: appReleases,
		Restart:  restartApp,
		Rollback: rollBack,
		Backup:   backupNow,
		Restore: func(ctx context.Context, server, app, database, to string, said io.Writer) (daemon.Restored, error) {
			return restoreCopy(ctx, server, app, database, to, "", nil, said)
		},
		Explain: explain,
	})
}
