package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

// password is `moin password [app]`: the app asks every visitor for a
// user and a password, on every address. `moin password off [app]` lets
// everyone in again. Both start the app again, without a build. The
// password is typed at a hidden prompt or comes on stdin, never on the
// command line.
func password(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	off := len(args) > 0 && args[0] == "off"
	if off {
		args = args[1:]
	}
	flags := newFlags("password", "password [app] [--user name] [-s server], or password off [app]").withServer()
	user := flags.String("user", daemon.PasswordUser, "the user visitors type with the password")
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	if off {
		return onApp(ctx, flags, app, func(server string) error {
			if _, err := runRelease(ctx, server, []string{"password", app, "--off"}, nil, stdout, "starting "+app+" without its password"); err != nil {
				return err
			}
			done(stdout, "%s lets everyone in again", app)
			return nil
		})
	}
	secret, err := readPassword(stdin, stdout, app)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		if _, err := runRelease(ctx, server, []string{"password", app, "--user", *user}, strings.NewReader(secret), stdout, "starting "+app+" with its password"); err != nil {
			return err
		}
		done(stdout, "%s asks every visitor for a password now. The user is %s", app, *user)
		fmt.Fprintln(stdout, "  "+style.Muted.Render("Webhooks and API clients of "+app+" need it too, like https://"+*user+":<password>@"+app+".example.com."))
		hint(stdout, "Let everyone in again with", build.Name+" password off "+app)
		return nil
	})
}

// readPassword asks for the app's new password twice without showing it,
// or reads all of stdin, without the line break at its end.
func readPassword(stdin io.Reader, stdout io.Writer, app string) (string, error) {
	if !isTerminal(stdin) {
		body, err := io.ReadAll(io.LimitReader(stdin, 4<<10))
		secret := strings.TrimRight(string(body), "\r\n")
		if err == nil && secret == "" {
			err = fmt.Errorf("no password. Type it when %s asks, or pipe it in: `echo \"$PASSWORD\" | %s password %s`", build.Name, build.Name, app)
		}
		return secret, err
	}
	var typed [2]string
	for index, question := range []string{"Password for " + app + ":", "Once more:"} {
		fmt.Fprint(stdout, style.Warning.Render("?")+" "+question+" ")
		secret, err := readSecret(stdin)
		fmt.Fprintln(stdout)
		if err != nil {
			return "", err
		}
		typed[index] = secret
	}
	switch {
	case typed[0] == "":
		return "", errors.New("no password, nothing changed")
	case typed[0] != typed[1]:
		return "", errors.New("the two are not the same, nothing changed")
	}
	return typed[0], nil
}

// herePassword is `here password <app> [--user name]`, with the password
// on stdin, and `here password <app> --off`.
func herePassword(ctx context.Context, dir string, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("here password", "here password <app> [--user name] [--off]")
	user := flags.String("user", "", "the user")
	off := flags.Bool("off", false, "no password any more")
	app, err := hereApp(flags, args)
	if err != nil {
		return err
	}
	if *off {
		value, err := daemon.RemovePasswordHere(ctx, dir, app, os.Stderr)
		return respond(stdout, value, err)
	}
	secret, err := io.ReadAll(io.LimitReader(stdin, 4<<10))
	if err != nil {
		return err
	}
	value, err := daemon.SetPasswordHere(ctx, dir, app, *user, strings.TrimRight(string(secret), "\r\n"), os.Stderr)
	return respond(stdout, value, err)
}
