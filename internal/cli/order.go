package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/style"
)

// moveUsage is where move puts an app or a server.
const moveUsage = "up|down|top|bottom|<position>"

// moveTo is names with name moved to where: up, down, top, bottom or a
// position from 1.
func moveTo(names []string, name, where string) ([]string, error) {
	from := slices.Index(names, name)
	if from < 0 {
		return nil, fmt.Errorf("there is no %s", name)
	}
	to := from
	switch where {
	case "up":
		to--
	case "down":
		to++
	case "top", "first":
		to = 0
	case "bottom", "last":
		to = len(names) - 1
	default:
		position, err := strconv.Atoi(where)
		if err != nil || position < 1 {
			return nil, fmt.Errorf("move it %s, or to a position from 1", moveUsage)
		}
		to = position - 1
	}
	return client.Moved(names, name, to), nil
}

// move puts one app or server of kind at a new place in the order of the
// workspace and prints the new order. list gives the names in the order
// now, reorder saves a new one.
func move(ctx context.Context, kind string, args []string, stdout io.Writer, apiErr error, list func(context.Context) ([]string, error), reorder func(context.Context, []string) ([]string, error)) error {
	flags := newFlags(kind+"s move", kind+"s move <"+kind+"> "+moveUsage)
	if err := flags.parse(args); err != nil {
		return err
	}
	if apiErr != nil {
		return apiErr
	}
	if flags.NArg() != 2 {
		return flags.wrong()
	}
	names, err := list(ctx)
	if err != nil {
		return err
	}
	name := flags.Arg(0)
	order, err := moveTo(names, name, flags.Arg(1))
	if err != nil {
		return err
	}
	if slices.Equal(order, names) {
		done(stdout, "%s stays at %d of %d: nothing changed", style.Bold.Render(name), slices.Index(order, name)+1, len(order))
		return nil
	}
	saved, err := reorder(ctx, order)
	if err != nil {
		return err
	}
	done(stdout, "Moved %s to %d of %d", style.Bold.Render(name), slices.Index(saved, name)+1, len(saved))
	fmt.Fprintln(stdout, "  "+style.Muted.Render(strings.Join(saved, ", ")))
	return nil
}

func moveApp(ctx context.Context, api *client.Client, apiErr error, args []string, stdout io.Writer) error {
	return move(ctx, "app", args, stdout, apiErr, func(ctx context.Context) ([]string, error) {
		apps, err := api.Apps(ctx)
		return appNames(apps), err
	}, func(ctx context.Context, order []string) ([]string, error) {
		apps, err := api.ReorderApps(ctx, order)
		return appNames(apps), err
	})
}

func moveServer(ctx context.Context, api *client.Client, apiErr error, args []string, stdout io.Writer) error {
	return move(ctx, "server", args, stdout, apiErr, func(ctx context.Context) ([]string, error) {
		servers, err := api.Servers(ctx)
		return serverNames(servers), err
	}, func(ctx context.Context, order []string) ([]string, error) {
		servers, err := api.ReorderServers(ctx, order)
		return serverNames(servers), err
	})
}

func appNames(apps []client.App) []string {
	names := make([]string, 0, len(apps))
	for _, app := range apps {
		names = append(names, app.Name)
	}
	return names
}

func serverNames(servers []client.Server) []string {
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}
	return names
}
