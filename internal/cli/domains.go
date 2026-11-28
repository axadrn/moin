package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

// domains is `moin domains [app]`, and `moin domains add|rm [app]
// <domain>`, which start the app again with its new domains, without a
// build.
func domains(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && (args[0] == "add" || args[0] == "rm") {
		return changeDomain(ctx, args[0], args[1:], stdin, stdout)
	}
	flags := newFlags("domains", "domains [app] [-s server] [--json], or domains add|rm [app] <domain> [--service <service>]").withServer().withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		var list []daemon.Domain
		if err := sshJSON(ctx, server, []string{"domains", app}, nil, &list); err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, list)
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, app+" has no domains yet, only its instant address.")
		}
		for _, line := range list {
			if line.Service == "" {
				fmt.Fprintln(stdout, line.Domain)
				continue
			}
			fmt.Fprintln(stdout, line.Domain+"  "+style.Muted.Render("to the service "+line.Service))
		}
		hint(stdout, "Add one with", build.Name+" domains add "+app+" "+app+".example.com")
		return nil
	})
}

// changeDomain adds or removes one domain. Left out, it is asked on a
// terminal. --service sends a new domain to another service than the main
// one.
func changeDomain(ctx context.Context, verb string, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("domains "+verb, "domains "+verb+" [app] <domain> [-s server]").withServer()
	service := new(string)
	if verb == "add" {
		flags.usage += " [--service <service>]"
		service = flags.String("service", "", "the service it reaches, the main one when left out")
	}
	if verb == "rm" {
		flags.usage += " [-y]"
		flags.withYes()
	}
	if err := flags.parse(args); err != nil {
		return err
	}
	// The domain may be left out, to be asked: an app name has no dot, a
	// domain has one.
	others := 1
	if flags.NArg() < 2 && !strings.Contains(flags.Arg(0), ".") {
		others = 0
	}
	app, rest, err := appArgs(flags, others)
	if err != nil {
		return err
	}
	var domain string
	switch {
	case len(rest) == 1:
		domain = rest[0]
	case !isTerminal(stdin):
		return flags.wrong()
	default:
		if domain, err = ask(stdin, stdout, "Domain, like "+app+".example.com:", ""); err != nil {
			return err
		}
		if domain == "" {
			return errors.New("no domain, nothing changed")
		}
	}
	if verb == "rm" {
		if err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Take %s off %s? Visitors there get no answer.", domain, app)); err != nil {
			return err
		}
	}
	return onApp(ctx, flags, app, func(server string) error {
		command := []string{"domains", verb, app, domain}
		if *service != "" {
			command = append(command, *service)
		}
		if _, err := runRelease(ctx, server, command, nil, stdout, "starting "+app+" with its domains"); err != nil {
			return err
		}
		if verb == "rm" {
			done(stdout, "%s no longer answers on %s", app, domain)
			return nil
		}
		if *service != "" {
			done(stdout, "%s answers on https://%s, its service %s", app, domain, *service)
		} else {
			done(stdout, "%s answers on https://%s", app, domain)
		}
		fmt.Fprintln(stdout, "  "+style.Muted.Render("Point the DNS of "+domain+" to "+server+": an A record with its IP. The certificate follows by itself."))
		return nil
	})
}
