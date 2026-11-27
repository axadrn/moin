package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
)

// knocksJSON is one server in `moin knocks --json`.
type knocksJSON struct {
	Server string         `json:"server"`
	Error  string         `json:"error,omitempty"`
	Knocks *daemon.Knocks `json:"knocks,omitempty"`
}

// knocks is `moin knocks [server]`: what the firewall of each server
// dropped, by the name of the port.
func knocks(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("knocks", "knocks [server] [--json]").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	var servers []string
	switch flags.NArg() {
	case 0:
		list, err := loadServers()
		if err != nil {
			return err
		}
		servers = list
	case 1:
		servers = []string{flags.Arg(0)}
	default:
		return flags.wrong()
	}
	results := make([]knocksJSON, len(servers))
	var wait sync.WaitGroup
	for i, server := range servers {
		wait.Go(func() {
			var counted daemon.Knocks
			results[i].Server = server
			if err := sshJSON(ctx, server, []string{"knocks"}, nil, &counted); err != nil {
				results[i].Error = explain(err)
				return
			}
			results[i].Knocks = &counted
		})
	}
	wait.Wait()
	if flags.json() {
		return printJSON(stdout, results)
	}
	if len(results) == 0 {
		fmt.Fprintln(stdout, "No servers yet.")
		return nil
	}
	for i, result := range results {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintln(stdout, style.Bold.Render(result.Server))
		switch {
		case result.Error != "":
			fmt.Fprintln(stdout, "  "+style.Muted.Render(result.Error))
			continue
		case result.Knocks.All.Total == 0:
			fmt.Fprintln(stdout, "  "+style.Muted.Render("No knocks counted. The firewall counts them once public SSH is closed."))
			hint(stdout, "Close it:", build.Name+" secure "+result.Server)
			continue
		}
		rows := [][]string{}
		for _, count := range append(result.Knocks.Names, result.Knocks.All) {
			rows = append(rows, []string{count.Name, human.Count(count.Hour), human.Count(count.Today), human.Count(count.Week), human.Count(count.Year), human.Count(count.Total), strconv.FormatFloat(count.PerMinute, 'f', 1, 64)})
		}
		table(stdout, []string{"PORT", "HOUR", "TODAY", "7 DAYS", "YEAR", "TOTAL", "PER MINUTE"}, rows)
		fmt.Fprintln(stdout, style.Muted.Render("All dropped. Counted since "+result.Knocks.Since.Local().Format("January 2, 2006")+"."))
	}
	return nil
}
