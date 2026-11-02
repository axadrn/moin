package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// activity prints what the team did, newest first, to the second in the
// local time zone. --app, --server and --user narrow it; --cursor continues
// with older events.
func activity(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("activity", "activity [--app <app>] [--server <server>] [--user <email>] [--limit 30] [--cursor <cursor>]")
	limit := flags.Int("limit", 30, "how many events, at most 200")
	app := flags.String("app", "", "only this app")
	server := flags.String("server", "", "only this server and its apps")
	user := flags.String("user", "", "only this member, by email")
	cursor := flags.String("cursor", "", "continue with older events, from the hint at the end")
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
	query := client.ActivityQuery{App: *app, Server: *server, User: *user, Cursor: *cursor, Limit: min(max(*limit, 1), 200)}
	page, err := api.Activity(ctx, query)
	if err != nil {
		return err
	}
	if len(page.Items) == 0 && (*app != "" || *server != "" || *user != "") {
		// A filter that names nothing, like a typo, finds nothing too.
		fmt.Fprintln(stdout, style.Muted.Render("No events match. Check the names, or run "+build.Name+" activity without filters."))
		return nil
	}
	if len(page.Items) == 0 {
		fmt.Fprintln(stdout, style.Muted.Render("Nothing happened yet."))
		return nil
	}
	rows := [][]string{}
	for _, event := range page.Items {
		rows = append(rows, []string{when(event.CreatedAt), event.Actor, viaName(event.Via, event.Token), event.Summary})
	}
	table(stdout, []string{"WHEN", "WHO", "VIA", "WHAT"}, rows)
	if page.Next != "" {
		next := build.Name + " activity"
		for _, flag := range [][2]string{{"app", *app}, {"server", *server}, {"user", *user}} {
			if flag[1] != "" {
				next += " --" + flag[0] + " " + flag[1]
			}
		}
		if *limit != 30 {
			next += fmt.Sprintf(" --limit %d", query.Limit)
		}
		hint(stdout, "Older events:", next+" --cursor "+page.Next)
	}
	return nil
}

// when is the time of an event to the second, in the local time zone.
func when(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04:05 MST")
}

// viaName names how a change came in, with the API token it came with.
func viaName(via, token string) string {
	name := via
	switch via {
	case "hook":
		name = "deploy hook"
	case "system":
		name = "ops"
	}
	if token != "" {
		return name + ", token " + token
	}
	return name
}

// team lists the members of the token's workspace, invites one, sends
// or revokes an invitation, or renames the workspace.
func team(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("team", "team, team invite <email> [--admin], team resend <email>, team revoke <email> [--yes], or team rename <name>").withYes()
	admin := flags.Bool("admin", false, "invite as admin, who may invite others")
	if err := flags.parse(args); err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	command, rest := flags.Arg(0), flags.Args()
	if len(rest) > 0 {
		rest = rest[1:]
	}
	switch command {
	case "":
	case "invite":
		if len(rest) != 1 {
			return fmt.Errorf("name the email: %s team invite ada@example.com", build.Name)
		}
		role := "member"
		if *admin {
			role = "admin"
		}
		err = api.Invite(ctx, rest[0], role)
		if err != nil {
			return err
		}
		done(stdout, "Invited %s as %s. The invitation is in their inbox for seven days.", rest[0], role)
		return nil
	case "rename":
		if len(rest) == 0 {
			return fmt.Errorf("name the new name: %s team rename \"Acme Inc\"", build.Name)
		}
		team, err := api.Team(ctx)
		if err != nil {
			return err
		}
		name := strings.Join(rest, " ")
		err = api.RenameWorkspace(ctx, team.Workspace.Slug, name)
		if err != nil {
			return err
		}
		done(stdout, "Renamed the workspace to %s. Its links stay the same.", name)
		return nil
	case "resend", "revoke":
		if len(rest) != 1 {
			return fmt.Errorf("name the email: %s team %s ada@example.com", build.Name, command)
		}
		team, err := api.Team(ctx)
		if err != nil {
			return err
		}
		index := slices.IndexFunc(team.Invitations, func(invitation client.Invitation) bool {
			return strings.EqualFold(invitation.Email, rest[0]) || invitation.ID == rest[0]
		})
		if index < 0 {
			return fmt.Errorf("no open invitation for %s. %s team shows them", rest[0], build.Name)
		}
		invitation := team.Invitations[index]
		if command == "resend" {
			err = api.ResendInvitation(ctx, team.Workspace.Slug, invitation.ID)
			if err != nil {
				return err
			}
			done(stdout, "Sent the invitation to %s again. Only the new link works.", invitation.Email)
			return nil
		}
		err = confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Revoke the invitation of %s? The link in the email stops working.", invitation.Email))
		if err != nil {
			return err
		}
		err = api.RevokeInvitation(ctx, team.Workspace.Slug, invitation.ID)
		if err != nil {
			return err
		}
		done(stdout, "Revoked the invitation of %s.", invitation.Email)
		return nil
	default:
		return fmt.Errorf("unknown team command %q: use %s team, team invite, team resend, team revoke or team rename", command, build.Name)
	}
	if len(rest) > 0 {
		return flags.wrong()
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
