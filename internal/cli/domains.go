package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/style"
)

// domains is `moin domains [app]`, and `moin domains add|rm [app]
// <domain>`, which start the app again with its new domains, without a
// build.
func domains(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && (args[0] == "add" || args[0] == "rm") {
		return changeDomain(ctx, args[0], args[1:], stdin, stdout)
	}
	flags := newFlags("domains", "domains [app] [-s server] [--json], or domains add|rm [app] <domain>").withServer().withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		var list []string
		if err := sshJSON(ctx, server, []string{"domains", app}, nil, &list); err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, list)
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, app+" has no domains yet, only its instant address.")
		}
		for _, domain := range list {
			fmt.Fprintln(stdout, domain)
		}
		hint(stdout, "Add one with", build.Name+" domains add "+app+" "+app+".example.com")
		return nil
	})
}

// changeDomain adds or removes one domain.
func changeDomain(ctx context.Context, verb string, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("domains "+verb, "domains "+verb+" [app] <domain> [-s server]").withServer()
	if verb == "rm" {
		flags.usage += " [-y]"
		flags.withYes()
	}
	if err := flags.parse(args); err != nil {
		return err
	}
	app, rest, err := appArgs(flags, 1)
	if err != nil {
		return err
	}
	domain := rest[0]
	if verb == "rm" {
		if err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Take %s off %s? Visitors there get no answer.", domain, app)); err != nil {
			return err
		}
	}
	return onApp(ctx, flags, app, func(server string) error {
		if _, err := runRelease(ctx, server, []string{"domains", verb, app, domain}, nil, stdout, "starting "+app+" with its domains"); err != nil {
			return err
		}
		if verb == "rm" {
			done(stdout, "%s no longer answers on %s", app, domain)
			return nil
		}
		done(stdout, "%s answers on https://%s", app, domain)
		fmt.Fprintln(stdout, "  "+style.Muted.Render("Point the DNS of "+domain+" to "+server+": an A record with its IP. The certificate follows by itself."))
		return nil
	})
}
