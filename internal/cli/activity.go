package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/axadrn/ops/internal/build"
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

// team lists the members of the token's workspace, or invites one.
func team(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("team", flag.ContinueOnError)
	admin := flags.Bool("admin", false, "invite as admin, who may invite others")
	if err := parseAnywhere(flags, args); err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	if flags.Arg(0) == "invite" {
		email := flags.Arg(1)
		if email == "" {
			return fmt.Errorf("name the email: %s team invite ada@example.com", build.Name)
		}
		role := "member"
		if *admin {
			role = "admin"
		}
		err = api.Invite(ctx, email, role)
		if err != nil {
			return err
		}
		done(stdout, "Invited %s as %s. The invitation is in their inbox for seven days.", email, role)
		return nil
	}
	if flags.Arg(0) != "" {
		return fmt.Errorf("unknown team command %q: use %s team or %s team invite <email>", flags.Arg(0), build.Name, build.Name)
	}
	team, err := api.Team(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, style.Bold.Render(team.Workspace.Name)+style.Muted.Render(" · you are "+team.Role))
	rows := [][]string{}
	for _, member := range team.Members {
		rows = append(rows, []string{member.Name, member.Email, member.Role})
	}
	for _, invitation := range team.Invitations {
		rows = append(rows, []string{style.Muted.Render("invited"), invitation.Email, invitation.Role})
	}
	table(stdout, []string{"NAME", "EMAIL", "ROLE"}, rows)
	return nil
}
