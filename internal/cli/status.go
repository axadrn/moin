package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/style"
)

// status says whether everything is OK in the workspace. It exits 1 when
// something needs you, so a script or CI can check it.
func status(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("status", "status")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	answer, err := api.Status(ctx)
	if err != nil {
		return err
	}
	printStatus(stdout, answer)
	if !answer.OK {
		return errReported
	}
	return nil
}

// printStatus writes what needs you first, then what could be newer, then
// the counts.
func printStatus(w io.Writer, status client.Status) {
	counts := fmt.Sprintf("%d of %d apps live · %d of %d servers online · %d of %d backups checked today",
		status.Counts.AppsLive, status.Counts.Apps, status.Counts.ServersOnline, status.Counts.Servers, status.Counts.BackupsChecked, status.Counts.Databases)
	if status.OK {
		fmt.Fprintln(w, style.Success.Render(style.SymbolDone)+" All good.")
		fmt.Fprintln(w, "  "+style.Muted.Render(counts))
	} else {
		need := "1 thing needs you"
		if len(status.Issues) != 1 {
			need = fmt.Sprintf("%d things need you", len(status.Issues))
		}
		fmt.Fprintln(w, style.Danger.Render(style.SymbolFailed)+" "+need+":")
		for _, issue := range status.Issues {
			mark := style.Danger.Render(style.SymbolFailed)
			if issue.Kind == "agent.behind" {
				mark = style.Warning.Render(style.SymbolRunning)
			}
			fmt.Fprintf(w, "  %s %s %s\n", mark, issue.Target, style.Muted.Render(issue.Detail))
		}
		fmt.Fprintln(w, "  "+style.Muted.Render(counts))
	}
	if len(status.Updates) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Updates:")
		for _, update := range status.Updates {
			fmt.Fprintf(w, "  %s %s %s → %s\n", update.App, style.Muted.Render(update.Name), update.Version, update.Latest)
		}
		hint(w, "Update one with", "moin update <app>")
	}
}
