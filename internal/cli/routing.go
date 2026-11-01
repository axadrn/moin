package cli

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
)

// domainCommands are the subcommands of domains. Any other first argument
// is the app whose domains to list.
var domainCommands = []string{"ls", "list", "add", "rm", "remove", "canonical"}

// domains lists the domains of an app with their DNS state, and adds,
// removes or picks the canonical one.
func domains(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	command := "ls"
	if len(args) > 0 && slices.Contains(domainCommands, args[0]) {
		command, args = args[0], args[1:]
	}
	usage := map[string]string{
		"add":       "domains add [--app <app>] <domain> ...",
		"rm":        "domains rm [--app <app>] <domain> ... [--yes]",
		"remove":    "domains rm [--app <app>] <domain> ... [--yes]",
		"canonical": "domains canonical [--app <app>] <domain|none>",
	}[command]
	if usage == "" {
		usage = "domains [app]"
	}
	flags := newFlags("domains "+command, usage).withApp()
	if command == "rm" || command == "remove" {
		flags.withYes()
	}
	if err := flags.parse(args); err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	if command == "ls" || command == "list" {
		if flags.NArg() > 1 {
			return flags.wrong()
		}
		name, err := flags.app(flags.Arg(0))
		if err != nil {
			return err
		}
		return printDomains(ctx, api, name, stdout)
	}
	name, err := flags.app("")
	if err != nil {
		return err
	}
	detail, err := api.App(ctx, name)
	if err != nil {
		return err
	}
	list, canonical := detail.App.Domains, detail.App.Canonical
	switch command {
	case "add":
		if flags.NArg() == 0 {
			return flags.wrong()
		}
		for _, domain := range flags.Args() {
			if !slices.Contains(list, strings.ToLower(domain)) {
				list = append(list, domain)
			}
		}
	case "rm", "remove":
		if flags.NArg() == 0 {
			return flags.wrong()
		}
		err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Remove %s from %s? Visitors there stop reaching it.", strings.Join(flags.Args(), ", "), name))
		if err != nil {
			return err
		}
		list = slices.DeleteFunc(list, func(domain string) bool { return slices.Contains(flags.Args(), domain) })
		if slices.Contains(flags.Args(), canonical) {
			canonical = ""
		}
	case "canonical":
		if flags.NArg() != 1 {
			return flags.wrong()
		}
		canonical = flags.Arg(0)
		if canonical == "none" {
			canonical = ""
		}
	}
	change, err := api.SetDomains(ctx, name, list, canonical)
	if err != nil {
		return err
	}
	reportChange(ctx, api, name, change, stdout)
	return printDomains(ctx, api, name, stdout)
}

func printDomains(ctx context.Context, api *client.Client, name string, stdout io.Writer) error {
	checks, err := api.CheckDomains(ctx, name)
	if err != nil {
		return err
	}
	if len(checks) == 0 {
		hint(stdout, name+" has no domains yet. Add one:", build.Name+" domains add blog.de www.blog.de")
		return nil
	}
	rows := make([][]string, 0, len(checks))
	advice := ""
	for _, check := range checks {
		advice = check.Advice
		state := badge(stdout, style.Badge(style.StatusLive, "points here"), "points here")
		if !check.Points {
			state = badge(stdout, style.Badge(style.StatusWaiting, "set DNS"), "set DNS")
		}
		role := ""
		if check.Canonical {
			role = "canonical"
		}
		rows = append(rows, []string{check.Domain, state, strings.Join(check.Found, " "), role})
	}
	table(stdout, []string{"DOMAIN", "DNS", "RESOLVES TO", ""}, rows)
	if advice != "" {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "  "+style.Muted.Render("Each domain needs an "+advice+"."))
	}
	return nil
}

// protect puts a password in front of an app, or takes it away with off.
func protect(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("protect", "protect [--user ops] [app], password on stdin; protect off [app]").withApp()
	user := flags.String("user", "ops", "username for the password prompt in the browser")
	if err := flags.parse(args); err != nil {
		return err
	}
	off := flags.Arg(0) == "off"
	argument := flags.Arg(0)
	if off {
		argument = flags.Arg(1)
	}
	if flags.NArg() > 2 || (!off && flags.NArg() > 1) {
		return flags.wrong()
	}
	name, err := flags.app(argument)
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	if off {
		change, err := api.RemoveProtection(ctx, name)
		if err != nil {
			return err
		}
		done(stdout, "%s is public again.", name)
		reportChange(ctx, api, name, change, stdout)
		return nil
	}
	if isTerminal(stdin) {
		fmt.Fprintf(stdout, "Password for %s (user %s): ", name, *user)
	}
	password, err := readSecret(stdin)
	if isTerminal(stdin) {
		fmt.Fprintln(stdout)
	}
	if err != nil {
		return err
	}
	change, err := api.SetProtection(ctx, name, *user, password)
	if err != nil {
		return err
	}
	done(stdout, "%s asks for user %s and the password now.", name, *user)
	reportChange(ctx, api, name, change, stdout)
	return nil
}

// restart starts the live release again with the env and routing as they
// are now; start runs a stopped app the same way.
func restart(ctx context.Context, command string, args []string, stdout io.Writer) error {
	flags := newFlags(command, command+" [app]").withApp()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	run, title := api.Restart, "Restart"
	if command == "start" {
		run, title = api.Start, "Start"
	}
	started, err := run(ctx, name)
	if err != nil {
		return err
	}
	return follow(ctx, api, name, started, title, false, stdout)
}

// stop takes the app offline until it starts again. Its data stays.
func stop(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("stop", "stop [app] [--yes]").withApp().withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	name, err := flags.app(flags.Arg(0))
	if err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	err = confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Stop %s? It is offline until you start it; its data stays.", name))
	if err != nil {
		return err
	}
	err = api.Stop(ctx, name)
	if err != nil {
		return err
	}
	done(stdout, "Stopped %s. It stays off, also after a reboot", style.Bold.Render(name))
	hint(stdout, "Run it again with", build.Name+" start "+name)
	return nil
}

// reportChange follows the deploy that applies a change, or says when it applies.
func reportChange(ctx context.Context, api *client.Client, name string, change client.Change, stdout io.Writer) {
	if change.Deploy == nil && change.App.Stopped {
		hint(stdout, "It applies when the app starts:", build.Name+" start "+name)
		return
	}
	if change.Deploy == nil {
		hint(stdout, "It applies with the next deploy:", build.Name+" deploy "+name)
		return
	}
	err := follow(ctx, api, name, *change.Deploy, "Restart", false, stdout)
	if err != nil {
		printError(stdout, err)
	}
}
