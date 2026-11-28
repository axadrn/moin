// Package tui is `moin` on a terminal: every server and its apps, live,
// with tabs for apps, servers and backups, the facts of the selected one
// beside the list, and the logs of an app. It is only a view. Every
// answer comes from the same functions as the CLI's commands, which the
// CLI hands it in CLI.
package tui

import (
	"context"
	"io"

	tea "charm.land/bubbletea/v2"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/protocol"
)

// CLI is what the terminal UI asks the CLI to do. Each one is the
// function a command calls. What ssh and moin say on the server goes to
// the writer each one takes, never to the screen.
type CLI struct {
	// Servers are the servers of your list, in its order.
	Servers []string
	// Status asks one server what runs there, as `moin` does.
	Status func(ctx context.Context, server string) (daemon.Status, error)
	// Logs writes the app's last lines and then what it writes, until ctx
	// ends, as `moin logs -f` does.
	Logs func(ctx context.Context, server, app string, out io.Writer) error
	// Backups is how the app's databases are backed up, as `moin backups`.
	Backups func(ctx context.Context, server, app string, said io.Writer) ([]protocol.DatabaseBackup, error)
	// Releases are the releases a rollback can go back to.
	Releases func(ctx context.Context, server, app string, said io.Writer) ([]daemon.Release, error)
	// Restart runs the live release again, as `moin restart`.
	Restart func(ctx context.Context, server, app string, log io.Writer) (daemon.Result, error)
	// Rollback runs an older release again, as `moin rollback --to`.
	Rollback func(ctx context.Context, server, app string, number int, log io.Writer) (daemon.Result, error)
	// Backup takes a copy of the app's databases now, as `moin backup`.
	Backup func(ctx context.Context, server, app string, said io.Writer) ([]daemon.Copied, error)
	// Restore puts a database of the app back to a copy, as `moin
	// restore --to`.
	Restore func(ctx context.Context, server, app, database, to string, said io.Writer) (daemon.Restored, error)
	// Explain turns an error into the sentence the CLI shows.
	Explain func(error) string
}

// Run shows the terminal UI until you quit it.
func Run(ctx context.Context, cli CLI) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	_, err := tea.NewProgram(newModel(ctx, cli), tea.WithContext(ctx)).Run()
	return err
}
