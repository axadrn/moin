package daemon

import (
	"log/slog"
	"log/syslog"

	"github.com/axadrn/moin/internal/build"
)

// LogToJournal sends the log of a `moin here …` command to the server's
// journal, where `journalctl -t moin` shows it, and not to your terminal:
// what you should see, the command says itself. A server without a
// journal keeps no log of commands.
func LogToJournal() {
	journal, err := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, build.Name)
	if err != nil {
		slog.SetDefault(slog.New(slog.DiscardHandler))
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(journal, nil)))
}
