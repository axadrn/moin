// Package cli is `moin <command>`. The CLI on your laptop does its work
// on your servers over SSH, where moin runs as `moin here …`.
package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

const usage = `moin: your app on your server. Backups checked every day.

Usage
  moin                     Every server you deployed to, every app, what needs you [--json]
  moin <command>           Run one command

Apps
  deploy                   Deploy this folder: deploy prod the first time, then deploy [--image shop:dev] [--catalog umami] [--app shop]
  apps                     Every app on every server [--json]
  rollback                 Go back to an older release, picked on a terminal: rollback [app] [--to v3]
  restart                  Start the app again, with its env and domains as they are now: restart [app]
  stop                     Stop the app until you start it: stop [app]
  start                    Start a stopped app: start [app]
  logs                     The app's logs: logs [app] [-f] [--tail 200]
  exec                     Run a command in the app's container: exec [app] -- psql
  env                      The names of the app's env vars, never a value: env [app] [--json]
  env edit                 Change them in $EDITOR; the app restarts with them: env edit [app]
  domains                  The app's domains: domains [app] [--json]
  domains add              Add one; the certificate follows by itself: domains add [app] shop.example.com
  domains rm               Remove one: domains rm [app] shop.example.com
  catalog                  Open source apps to deploy with deploy --catalog [--json]

Backups
  backups                  The copies of the app's databases and the last restore check: backups [app] [--json]
  backup                   A copy now: backup [app]
  restore                  Go back to a copy, picked on a terminal; can be undone: restore [app] [--to <copy | 2026-11-27T14:07 | undo-…>]

Servers
  servers                  Your servers and their watchers [--json]
  servers add              Add one you deployed to before, as on a new laptop: servers add prod
  servers rm               Forget one; nothing changes on it: servers rm prod
  keys                     The team: the SSH keys that may log in as root: keys prod [--json]
  keys add                 Let one more key in: keys add prod anna.pub
  keys rm                  Take a key out; never the one you are logged in with: keys rm prod anna@laptop
  bucket                   Where the backups go: bucket prod [--json]
  bucket set               Set it; asks for each value: bucket set prod
  alerts                   Where alerts go: alerts prod [--json]
  alerts add               Add one: alerts add prod telegram --token … --chat-id …, or slack|discord|webhook --url …
  alerts rm                Remove one: alerts rm prod 2
  alerts test              Send a test alert: alerts test prod
  update                   Update this CLI to the newest release
  update <server>          Put this CLI's version of moin on a server: update prod

Watcher
  watcher run              Run a watcher, on another machine than your servers
  watcher token            On the watcher: the line that connects a server: watcher token prod --url https://watcher.example.com
  watcher connect          On your laptop: the server reports there: watcher connect prod <url> <token>
  watcher disconnect       It no longer does: watcher disconnect prod <url>

On the server
  daemon                   Run the timers: backups, restore checks, reports
  daemon uninstall         Remove moin from this server; the apps keep running: daemon uninstall [--apps] [-y]

  version                  Show the version
  help                     Show this help

A server is a name from your ssh config, like prod, or user@host: if
ssh prod works, moin works. Name the app, or leave it out in its folder;
moin finds its server. When it runs on two, add -s prod. Every command
shows its flags with --help.

Docs: https://moin.run/docs
`

// printUsage writes the help: section names bold, descriptions muted on a
// terminal, all in one column after the longest command.
func printUsage(w io.Writer) {
	lines := strings.Split(strings.TrimRight(usage, "\n"), "\n")
	width := 0
	for _, line := range lines {
		if name, _, ok := usageEntry(line); ok {
			width = max(width, len(name))
		}
	}
	for index, line := range lines {
		name, description, ok := usageEntry(line)
		switch {
		case index == 0:
			fmt.Fprintln(w, style.Accent.Render(build.Name)+strings.TrimPrefix(line, build.Name))
		case ok:
			fmt.Fprintln(w, "  "+name+strings.Repeat(" ", width-len(name)+2)+style.Muted.Render(description))
		case isHeading(line):
			fmt.Fprintln(w, style.Bold.Render(line))
		default:
			fmt.Fprintln(w, line)
		}
	}
}

// isHeading reports whether a line of the usage is a section name, like
// "Apps" or "On the server".
func isHeading(line string) bool {
	return line != "" && !strings.HasPrefix(line, " ") && !strings.ContainsAny(line, ":.,")
}

// usageEntry splits an indented line of the usage into the command and what
// it does, at the first two spaces.
func usageEntry(line string) (name, description string, ok bool) {
	if !strings.HasPrefix(line, "  ") {
		return "", "", false
	}
	name, description, ok = strings.Cut(strings.TrimPrefix(line, "  "), "  ")
	return name, strings.TrimSpace(description), ok
}

// Run runs one command and returns the exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stdout, stderr = terminal(stdout), terminal(stderr)
	if checksUpdates(args) && isTTY(stderr) {
		warnNewer(ctx, stderr)
	}
	err := run(ctx, args, stdin, stdout)
	var help helpError
	var code exitCode
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.As(err, &help):
		help.print(stdout)
		return 0
	case errors.As(err, &code):
		// The command said why itself, on the server.
		return int(code)
	case errors.Is(err, daemon.ErrNoApp) && args[0] == "here":
		// Quiet: the CLI looks on your other servers, and says it if none
		// runs the app.
		return noAppCode
	case errors.Is(err, errReported):
	case ctx.Err() != nil:
		fmt.Fprintln(stderr)
		printError(stderr, errors.New("stopped"))
	default:
		printError(stderr, err)
	}
	return 1
}

// checksUpdates reports whether the command looks for a newer release
// first: the commands you type on your laptop, not those of a server or
// a watcher.
func checksUpdates(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "here", "daemon", "version", "--version", "update":
		return false
	case "watcher":
		return len(args) > 1 && (args[1] == "connect" || args[1] == "disconnect")
	}
	return true
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "--json") {
		return overview(ctx, "", args, stdout)
	}
	command, rest := args[0], args[1:]
	switch command {
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	case "version", "--version":
		return version(rest, stdout)
	case "update":
		return update(ctx, rest, stdout)
	case "servers":
		return servers(ctx, rest, stdout)
	case "deploy":
		return deploy(ctx, rest, stdin, stdout)
	case "apps":
		return apps(ctx, rest, stdout)
	case "rollback":
		return rollback(ctx, rest, stdin, stdout)
	case "restart", "start", "stop":
		return restart(ctx, command, rest, stdout)
	case "logs":
		return logs(ctx, rest, stdout)
	case "exec":
		return execCommand(ctx, rest, stdin, stdout)
	case "env":
		return env(ctx, rest, stdin, stdout)
	case "domains":
		return domains(ctx, rest, stdout)
	case "keys":
		return keys(ctx, rest, stdin, stdout)
	case "catalog":
		return catalogList(ctx, rest, stdout)
	case "backups":
		return backups(ctx, rest, stdout)
	case "backup":
		return backUp(ctx, rest, stdout)
	case "restore":
		return restore(ctx, rest, stdin, stdout)
	case "bucket":
		return bucket(ctx, rest, stdin, stdout)
	case "alerts":
		return alerts(ctx, rest, stdin, stdout)
	case "watcher":
		if len(rest) > 0 && (rest[0] == "connect" || rest[0] == "disconnect") {
			return watchers(ctx, rest, stdout)
		}
		return watcherCommand(ctx, rest, stdout)
	case "here":
		return here(ctx, rest, stdin, stdout)
	case "daemon":
		return daemonCommand(ctx, rest, stdin, stdout)
	}
	return fmt.Errorf("unknown command %q. Run `%s help` for all of them", command, build.Name)
}

// noArguments parses the flags of a command that takes none, so --help
// shows its usage instead of running it.
func noArguments(command string, args []string) error {
	flags := newFlags(command, command)
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	return nil
}

// readLine reads one line and trims it.
func readLine(stdin io.Reader) (string, error) {
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// daemonCommand is `moin daemon` on a server, and `moin daemon uninstall`.
func daemonCommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	dir := daemon.StateDir()
	if len(args) > 0 && args[0] == "uninstall" {
		return uninstallDaemon(ctx, dir, args[1:], stdin, stdout)
	}
	if err := noArguments("daemon", args); err != nil {
		return err
	}
	return daemon.Run(ctx, dir)
}

// uninstallDaemon removes moin from this server and says what stays.
func uninstallDaemon(ctx context.Context, dir string, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("daemon uninstall", "daemon uninstall [--apps] [--yes]").withYes()
	withApps := flags.Bool("apps", false, "remove the apps with their data, the proxy, the tunnel and the firewall too")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	question := "Remove moin from this server? The apps keep running."
	if *withApps {
		question = "Remove moin and every app on this server with its data? This cannot be undone."
	}
	if err := confirm(stdin, stdout, flags.yes(), question); err != nil {
		return err
	}
	kept, err := daemon.Uninstall(ctx, dir, *withApps, func(format string, args ...any) { step(stdout, format, args...) })
	if err != nil {
		return err
	}
	done(stdout, "moin is removed from this server")
	if len(kept) > 0 {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("Still here: "+strings.Join(kept, "; ")+"."))
	}
	if !*withApps {
		hint(stdout, "Remove the apps and their data too, as root, with", build.Name+" daemon uninstall --apps")
	}
	return nil
}
