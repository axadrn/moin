package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
)

// uptime shows the uptime of an app, or changes the check of one domain:
// uptime path, pause and resume.
func uptime(ctx context.Context, args []string, stdout io.Writer) error {
	command := "show"
	if len(args) > 0 && slices.Contains([]string{"path", "pause", "resume"}, args[0]) {
		command, args = args[0], args[1:]
	}
	usage := map[string]string{
		"path":   "uptime path [--app <app>] <domain> <path|default>",
		"pause":  "uptime pause [--app <app>] <domain>",
		"resume": "uptime resume [--app <app>] <domain>",
	}[command]
	if usage == "" {
		usage = "uptime [app]"
	}
	flags := newFlags("uptime "+command, usage).withApp()
	if err := flags.parse(args); err != nil {
		return err
	}
	want := map[string]int{"show": 1, "path": 2, "pause": 1, "resume": 1}[command]
	if flags.NArg() > want || (command != "show" && flags.NArg() != want) {
		return flags.wrong()
	}
	argument := ""
	if command == "show" {
		argument = flags.Arg(0)
	}
	name, err := flags.app(argument)
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	if command == "show" {
		answer, err := api.Uptime(ctx, name)
		if err != nil {
			return err
		}
		printUptime(stdout, answer, time.Now())
		return nil
	}
	domain := flags.Arg(0)
	var path *string
	var paused *bool
	switch command {
	case "path":
		value := flags.Arg(1)
		if value == "default" {
			value = ""
		}
		path = &value
	default:
		value := command == "pause"
		paused = &value
	}
	answer, err := api.SetUptimeCheck(ctx, name, domain, path, paused)
	if err != nil {
		return err
	}
	switch command {
	case "path":
		done(stdout, "The check of %s goes to %s now.", answer.Domain, answer.Path)
	case "pause":
		done(stdout, "Paused the check of %s. Resume it with `%s uptime resume %s`.", answer.Domain, build.Name, answer.Domain)
	default:
		done(stdout, "Checking %s every minute again.", answer.Domain)
	}
	return nil
}

func printUptime(w io.Writer, answer client.Uptime, now time.Time) {
	if len(answer.Domains) == 0 {
		fmt.Fprintf(w, "%s has no domains to check. Add one: %s\n", answer.App, command(w, build.Name+" domains add blog.de"))
		return
	}
	rows := [][]string{}
	var outages [][]string
	for _, domain := range answer.Domains {
		check := "-"
		if domain.CheckedAt != nil {
			check = strconv.Itoa(domain.StatusCode) + " in " + strconv.Itoa(domain.ResponseMS) + " ms"
			if domain.Error != "" {
				check = domain.Error
			}
		}
		cert := "-"
		if domain.CertExpiresAt != nil {
			cert = strconv.Itoa(int(domain.CertExpiresAt.Sub(now).Hours()/24)) + " days"
		}
		rows = append(rows, []string{domain.Domain, uptimeState(domain.State), client.Percent(domain.Uptime24h), client.Percent(domain.Uptime7d), client.Percent(domain.Uptime30d), check, cert, domain.Path})
		for _, outage := range domain.Outages {
			lasted := "ongoing, " + human.Duration(now.Sub(outage.StartedAt))
			if outage.EndedAt != nil {
				lasted = human.Duration(outage.EndedAt.Sub(outage.StartedAt))
			}
			outages = append(outages, []string{domain.Domain, outage.StartedAt.UTC().Format("2006-01-02 15:04 UTC"), lasted, outage.Reason})
		}
	}
	table(w, []string{"DOMAIN", "STATE", "24H", "7D", "30D", "NEWEST CHECK", "CERT", "PATH"}, rows)
	if !answer.ServerOnline && answer.Server != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, style.Danger.Render(style.SymbolFailed)+" The server "+answer.Server+" is offline: its agent does not answer.")
	}
	if len(outages) > 0 {
		slices.SortFunc(outages, func(a, b []string) int { return cmp.Compare(b[1], a[1]) })
		fmt.Fprintln(w)
		fmt.Fprintln(w, style.Bold.Render("Last outages"))
		table(w, []string{"DOMAIN", "STARTED", "LASTED", "REASON"}, outages[:min(len(outages), 10)])
	}
}

// uptimeState colors a state: up green, down red, the rest muted.
func uptimeState(state string) string {
	switch state {
	case "up":
		return style.Success.Render(state)
	case "down":
		return style.Danger.Render(state)
	}
	return style.Muted.Render(state)
}
