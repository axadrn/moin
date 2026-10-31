package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
	"golang.org/x/term"
)

// domains lists the domains of an app with their DNS state, and adds,
// removes or picks the canonical one.
func domains(ctx context.Context, args []string, stdout io.Writer) error {
	command := "ls"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("domains "+command, flag.ContinueOnError)
	appFlag := flags.String("app", "", "app name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	name := appName(*appFlag, ".")
	if command == "ls" || command == "list" {
		return printDomains(ctx, api, name, stdout)
	}
	detail, err := api.App(ctx, name)
	if err != nil {
		return err
	}
	list, canonical := detail.App.Domains, detail.App.Canonical
	switch command {
	case "add":
		if flags.NArg() == 0 {
			return fmt.Errorf("usage: %s domains add [--app <app>] <domain> ...", build.Name)
		}
		for _, domain := range flags.Args() {
			if !slices.Contains(list, strings.ToLower(domain)) {
				list = append(list, domain)
			}
		}
	case "rm", "remove":
		if flags.NArg() == 0 {
			return fmt.Errorf("usage: %s domains rm [--app <app>] <domain> ...", build.Name)
		}
		list = slices.DeleteFunc(list, func(domain string) bool { return slices.Contains(flags.Args(), domain) })
		if slices.Contains(flags.Args(), canonical) {
			canonical = ""
		}
	case "canonical":
		if flags.NArg() != 1 {
			return fmt.Errorf("usage: %s domains canonical [--app <app>] <domain|none>", build.Name)
		}
		canonical = flags.Arg(0)
		if canonical == "none" {
			canonical = ""
		}
	default:
		return fmt.Errorf("unknown command %q. Run `%s help`", "domains "+command, build.Name)
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
	address := ""
	for _, check := range checks {
		address = check.Address
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
	if address != "" {
		fmt.Fprintln(stdout)
		hint(stdout, "Point an A record of each domain at", address)
	}
	return nil
}

// protect puts a password in front of an app, or takes it away with off.
func protect(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("protect", flag.ContinueOnError)
	appFlag := flags.String("app", "", "app name")
	user := flags.String("user", "ops", "username for the password prompt in the browser")
	if err := flags.Parse(args); err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	name := appName(*appFlag, ".")
	if flags.Arg(0) == "off" {
		change, err := api.RemoveProtection(ctx, name)
		if err != nil {
			return err
		}
		done(stdout, "%s is public again.", name)
		reportChange(ctx, api, name, change, stdout)
		return nil
	}
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fmt.Fprintf(stdout, "Password for %s (user %s): ", name, *user)
	}
	password, err := readSecret(stdin)
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
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

// apply restarts the running release with the env and routing as they are now.
func apply(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: %s apply [app]", build.Name)
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	name := appName(strings.Join(args, ""), ".")
	started, err := api.Apply(ctx, name)
	if err != nil {
		return err
	}
	return follow(ctx, api, name, started, "Apply", false, stdout)
}

// reportChange follows the deploy that applies a change, or says when it applies.
func reportChange(ctx context.Context, api *client.Client, name string, change client.Change, stdout io.Writer) {
	if change.Deploy == nil {
		hint(stdout, "It applies with the next deploy:", build.Name+" deploy "+name)
		return
	}
	err := follow(ctx, api, name, *change.Deploy, "Apply", false, stdout)
	if err != nil {
		printError(stdout, err)
	}
}
