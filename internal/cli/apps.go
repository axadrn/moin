package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/style"
)

// apps is `moin apps [server]`: the apps on one server, or on all.
func apps(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("apps", "apps [server] [--json]").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		return overview(ctx, args, stdout)
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	status, err := serverStatus(ctx, server)
	if err != nil {
		return err
	}
	rememberApps([]serverRow{{server: server, status: status}})
	if flags.json() {
		return printJSON(stdout, status.Apps)
	}
	if len(status.Apps) == 0 {
		fmt.Fprintln(stdout, "No apps on "+server+" yet.")
		hint(stdout, "Deploy one from its folder with", build.Name+" deploy "+server)
		return nil
	}
	printApps(stdout, []serverRow{{server: server, status: status}})
	return nil
}

// rollback is `moin rollback [app] [--to v3]`: an older release runs
// again, with the env and domains as they are now.
func rollback(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("rollback", "rollback [app] [--to v3] [-s server] [--json]").withServer().withJSON()
	to := flags.String("to", "", "the release to go back to, like v3; default the one before the live one")
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	command := []string{"rollback", app}
	if *to != "" {
		number, err := releaseNumber(*to)
		if err != nil {
			return err
		}
		command = append(command, "--to", strconv.Itoa(number))
	}
	return onApp(ctx, flags, app, func(server string) error {
		result, err := runRelease(ctx, server, command, nil, stdout, "rolling back "+app)
		if err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, result)
		}
		done(stdout, "%s runs v%d again, as v%d", app, result.From, result.Release)
		return nil
	})
}

// releaseNumber reads a release like v3 or 3.
func releaseNumber(text string) (int, error) {
	number, err := strconv.Atoi(strings.TrimPrefix(text, "v"))
	if err != nil || number < 1 {
		return 0, fmt.Errorf("%q is not a release: name one like v3", text)
	}
	return number, nil
}

// restart is `moin restart|start|stop [app]`. Restart and start run the
// live release again, with the env and domains as they are now; stop
// stops it until the next start.
func restart(ctx context.Context, command string, args []string, stdout io.Writer) error {
	flags := newFlags(command, command+" [app] [-s server]").withServer()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		if command == "stop" {
			if err := sshRun(ctx, server, hereLine("stop", app), nil, io.Discard, os.Stderr); err != nil {
				return err
			}
			done(stdout, "%s is stopped. It stays stopped after a reboot, until `%s start %s`", app, build.Name, app)
			return nil
		}
		result, err := runRelease(ctx, server, []string{"restart", app}, nil, stdout, "starting "+app)
		if err != nil {
			return err
		}
		done(stdout, "%s runs v%d", app, result.Release)
		return nil
	})
}

// logs is `moin logs [app] [-f]`: the last lines of the app's containers,
// and with -f what they write from then on.
func logs(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("logs", "logs [app] [-f] [--tail 200] [-s server]").withServer()
	tail := flags.Int("tail", 200, "how many lines from the end")
	follow := flags.Bool("f", false, "follow: keep printing new lines until Ctrl-C")
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	command := []string{"logs", app, "--tail", strconv.Itoa(*tail)}
	if *follow {
		command = append(command, "-f")
	}
	return onApp(ctx, flags, app, func(server string) error {
		err := sshRun(ctx, server, hereLine(command...), nil, stdout, os.Stderr)
		if ctx.Err() != nil {
			// Ctrl-C ends -f: that is how it is meant to stop.
			return nil
		}
		return err
	})
}

// execArgs splits `exec [app] [flags] -- <command…>` at the first "--":
// what comes before is moin's, what comes after is the command's own,
// flags and all.
func execArgs(args []string) (own, command []string) {
	cut := slices.Index(args, "--")
	if cut < 0 {
		return args, nil
	}
	return args[:cut], args[cut+1:]
}

// execCommand is `moin exec [app] -- <command>`: it runs a command in the
// app's container, on a terminal like a shell, and exits with its code.
func execCommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	own, command := execArgs(args)
	flags := newFlags("exec", "exec [app] [--service web] [-s server] -- <command> [args]").withServer()
	service := flags.String("service", "", "the compose service to run it in; default the app's main service")
	if err := flags.parse(own); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	if len(command) == 0 {
		return fmt.Errorf("name the command after --, like `%s exec %s -- ls`", build.Name, app)
	}
	tty := isTerminal(stdin)
	line := []string{"exec", app}
	if *service != "" {
		line = append(line, "--service", *service)
	}
	if tty {
		line = append(line, "--tty")
	}
	line = append(append(line, "--"), command...)
	return onApp(ctx, flags, app, func(server string) error {
		cmd := sshCommand(ctx, server, asRoot(hereLine(line...)), tty)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, os.Stderr
		return sshError(server, cmd.Run())
	})
}

// appState is how an app runs, in words and with its color on a terminal.
func appState(w io.Writer, running bool) string {
	if running {
		return badge(w, style.Badge(style.StatusLive, "running"), "running")
	}
	return badge(w, style.Badge(style.StatusIdle, "stopped"), "stopped")
}
