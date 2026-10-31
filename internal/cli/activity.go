package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/axadrn/ops/internal/style"
)

// activity prints what the team did, newest first.
func activity(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("activity", flag.ContinueOnError)
	limit := flags.Int("limit", 30, "how many events, at most 200")
	if err := parseAnywhere(flags, args); err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	events, err := api.Activity(ctx, min(max(*limit, 1), 200))
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Fprintln(stdout, style.Muted.Render("Nothing happened yet."))
		return nil
	}
	now := time.Now()
	rows := [][]string{}
	for _, event := range events {
		rows = append(rows, []string{since(event.CreatedAt, now), event.Actor, viaName(event.Via), event.Summary})
	}
	table(stdout, []string{"WHEN", "WHO", "VIA", "WHAT"}, rows)
	return nil
}

// viaName names how a change came in, as the web page does.
func viaName(via string) string {
	switch via {
	case "web":
		return "web"
	case "api":
		return "cli"
	case "mcp":
		return "agent"
	case "hook":
		return "deploy hook"
	case "github":
		return "github"
	}
	return ""
}
