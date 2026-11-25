package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
)

// connections lists the connections and CI tokens of the workspace, and
// removes one. Rights and new CI tokens are set in the web, signed in:
// no login gives itself more.
func connections(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "rm" {
		return removeConnection(ctx, args[1:], stdin, stdout)
	}
	flags := newFlags("connections", "connections [rm <id>]")
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
	connections, err := api.Connections(ctx)
	if err != nil {
		return err
	}
	tokens, err := api.CITokens(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	rows := [][]string{}
	for _, connection := range connections {
		name := connection.ClientName
		if connection.Current {
			name += style.Muted.Render(" (this login)")
		}
		rows = append(rows, []string{connection.ID, "connection", name, strings.Join(connection.Rights, ","), used(connection.LastUsedAt, now)})
	}
	for _, token := range tokens {
		name := token.Name
		if token.App != nil {
			name += style.Muted.Render(" (only " + *token.App + ")")
		}
		if token.Expired {
			name += style.Danger.Render(" expired")
		}
		rows = append(rows, []string{token.ID, "ci token", name, strings.Join(token.Rights, ","), used(token.LastUsedAt, now)})
	}
	if len(rows) == 0 {
		hint(stdout, "Nothing is connected. Connect this computer with", build.Name+" login")
		return nil
	}
	table(stdout, []string{"ID", "KIND", "NAME", "RIGHTS", "USED"}, rows)
	hint(stdout, "Change rights and make CI tokens in the web: Settings, Connections. Remove one with", build.Name+" connections rm <id>")
	return nil
}

func used(at *time.Time, now time.Time) string {
	if at == nil {
		return "never"
	}
	return human.Ago(*at, now)
}

// removeConnection ends a connection or deletes a CI token, by its ID,
// after a yes.
func removeConnection(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("connections rm", "connections rm <id> [--yes]")
	yes := flags.Bool("yes", false, "remove without asking")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return flags.wrong()
	}
	id := flags.Arg(0)
	api, err := apiClient()
	if err != nil {
		return err
	}
	connections, err := api.Connections(ctx)
	if err != nil {
		return err
	}
	for _, connection := range connections {
		if connection.ID != id {
			continue
		}
		question := "Remove " + connection.ClientName + "? It stops working right away."
		if connection.Current {
			question = "Remove this login? This computer is logged out."
		}
		if err := confirm(stdin, stdout, *yes, question); err != nil {
			return err
		}
		if err := api.RemoveConnection(ctx, id); err != nil {
			return err
		}
		done(stdout, "Removed %s", connection.ClientName)
		return nil
	}
	tokens, err := api.CITokens(ctx)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		if token.ID != id {
			continue
		}
		if err := confirm(stdin, stdout, *yes, "Remove the CI token "+token.Name+"? Requests with it stop right away."); err != nil {
			return err
		}
		if err := api.RemoveCIToken(ctx, id); err != nil {
			return err
		}
		done(stdout, "Removed the CI token %s", token.Name)
		return nil
	}
	return fmt.Errorf("no connection or CI token has the ID %s. Run `%s connections` for them", id, build.Name)
}
