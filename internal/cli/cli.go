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

Start
  moin                         Every server. Every app. What needs you
  deploy <server>              Deploy this folder. Name the server the first time

Apps
  deploy                       Deploy this folder again where it runs
  apps                         Every app on every server
  apps rm <app>                Remove an app. Its volumes stay
  logs [app]                   The app's logs. -f follows them
  deploys [app]                Its deploys: from where, what, how they ended
  deploys [app] v12            What deploy v12 printed
  rollback [app]               Go back to an older deploy
  restart [app]                Start the app again with its env and domains as now
  stop [app]                   Stop the app until you start it
  start [app]                  Start a stopped app
  exec [app] -- <command>      Run a command in the app's container
  env [app]                    The names of its env vars, never a value
  env edit [app]               Change them in $EDITOR. The app restarts with them
  env push [app] [file|-]      Replace them with a .env file. Shows the names first
  env set [app] <NAME>         Set one. The value is asked or read from stdin
  domains [app]                The app's domains
  domains add [app] <domain>   Add one. The certificate follows by itself. --service api for another service
  domains rm [app] <domain>    Remove one
  password [app]               Ask every visitor for a password. off ends it
  sql [app] "<query>"          Ask its database. --tables lists its tables
  follow [app]                 Deploy each new commit of a branch or image of a tag
  compose [app]                The compose file moin runs, to run it elsewhere
  catalog                      Open source apps to deploy with --catalog

Backups
  backups [app]                The copies of its databases and the last check
  backup [app]                 A copy now
  restore [app]                Go back to a copy. It can be undone
  backups [app] --every 1h     How often a copy. --keep 30d how long. -s alone: the server's

  backup download [app]        One copy, opened with your backup key, as a file
Servers
  servers                      Your servers and their watchers
  servers add <server>         Add one you deployed to before. As on a new laptop
  servers rm <server>          Forget one. Nothing changes on it
  keys <server>                The SSH keys that may log in as root
  keys add <server> <file>     Let one more key in
  keys rm <server> <name>      Take a key out
  bucket <server>              Where the backups go
  bucket set <server>          Set it. Shows your backup key once
  bucket key <server>          A new backup key or one you have
  bucket confirm <server>      Paste your backup key to show you have it
  registry <server>            The registries it is logged in to for private images
  registry set <server> <registry> <user>  Log it in. The token is asked
  registry rm <server> <registry>  Log it out
  alerts <server>              Where alerts go
  alerts add <server> <kind>   telegram, slack, discord, webhook or email
  alerts rm <server> <n>       Remove one
  alerts test <server>         Send a test alert
  secure <server>              SSH only over Tailscale. A firewall for the rest
  knocks [server]              What the firewall dropped, by port
  update [server]              Update this CLI, or a server to this CLI's version

Watcher
  watcher                      Each server's watchers. Your plan of moin cloud
  watcher connect <server>     The server reports to moin cloud. Signs you in
  watcher connect <server> <url> <token>  Or to a watcher you run
  watcher disconnect <server> [url]  It no longer does
  watcher run                  Run a watcher on another machine than your servers
  watcher token <server>       On the watcher: the line that connects a server
  watcher logout               Sign this laptop out of moin cloud

On the server
  unlock                       Open port 22 again from your provider's console
  daemon uninstall             Remove moin. The apps keep running

  version                      Show the version
  help                         Show this help

A server is a name from your ssh config, like my-server, or user@host. If
ssh my-server works, moin works. Leave the app out in its folder. When it
runs on two servers, add -s production. Every reading command takes --json.
Every command shows its flags with --help.

Docs: https://moin.run/docs
`

// usageColumn is where the descriptions of the help start. A longer
// command gets its description on the next line.
const usageColumn = 31

// printUsage writes the help: section names bold, commands in one
// column, descriptions muted after it.
func printUsage(w io.Writer) {
	for index, line := range strings.Split(strings.TrimRight(usage, "\n"), "\n") {
		name, description, ok := usageEntry(line)
		switch {
		case index == 0:
			fmt.Fprintln(w, style.Mark.Render(build.Name)+"  "+strings.TrimPrefix(line, build.Name+": "))
		case ok && len(name)+2 >= usageColumn:
			fmt.Fprintln(w, "  "+name)
			fmt.Fprintln(w, strings.Repeat(" ", usageColumn)+style.Muted.Render(description))
		case ok:
			fmt.Fprintln(w, "  "+name+strings.Repeat(" ", usageColumn-2-len(name))+style.Muted.Render(description))
		case isHeading(line):
			fmt.Fprintln(w, style.Bold.Render(line))
		default:
			fmt.Fprintln(w, style.Muted.Render(line))
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
	// SIGHUP too: a closed terminal still lets env edit remove its file.
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals...)
	defer stop()

	stdout, stderr = terminal(stdout), terminal(stderr)
	if checksUpdates(args) && isTTY(stderr) {
		warnNewer(ctx, stderr)
	}
	err := run(ctx, args, stdin, stdout)
	onServer := len(args) > 0 && args[0] == "here"
	var help helpError
	var code exitCode
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.As(err, &help):
		help.print(stdout)
		return 0
	case errors.Is(err, errStopped):
		// The cursor waits behind the question.
		fmt.Fprintln(stderr)
	case ctx.Err() != nil && !onServer:
		// Ctrl-C ends the ssh session, not the work on the server.
		fmt.Fprintln(stderr)
		printError(stderr, fmt.Errorf("stopped watching. It goes on on the server: `%s`", build.Name))
	case errors.As(err, &code):
		// The command said why itself, on the server.
		return int(code)
	case errors.Is(err, daemon.ErrNoApp) && onServer:
		// Quiet: the CLI looks on your other servers, and says it if none
		// runs the app.
		return noAppCode
	case errors.Is(err, errReported):
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
	case "here", "daemon", "unlock", "version", "--version", "update":
		return false
	case "watcher":
		return len(args) == 1 || args[1] != "run" && args[1] != "token"
	}
	return true
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 && isTTY(stdout) && isTerminal(stdin) {
		return openTUI(ctx, stdout)
	}
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
		return servers(ctx, rest, stdin, stdout)
	case "deploy":
		return deploy(ctx, rest, stdin, stdout)
	case "apps":
		return apps(ctx, rest, stdin, stdout)
	case "compose":
		return compose(ctx, rest, stdout)
	case "deploys":
		return deploys(ctx, rest, stdout)
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
		return domains(ctx, rest, stdin, stdout)
	case "password":
		return password(ctx, rest, stdin, stdout)
	case "sql":
		return sqlCommand(ctx, rest, stdin, stdout)
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
		if len(rest) > 0 && rest[0] == "download" {
			return backupDownload(ctx, rest[1:], stdin, stdout)
		}
		return alerts(ctx, rest, stdin, stdout)
	case "secure":
		return secure(ctx, rest, stdin, stdout)
	case "unlock":
		return unlock(ctx, rest, stdout)
	case "follow":
		return follow(ctx, rest, stdin, stdout)
	case "registry":
		return registry(ctx, rest, stdin, stdout)
	case "knocks":
		return knocks(ctx, rest, stdout)
	case "watcher":
		return watcherCommand(ctx, rest, stdin, stdout)
	case "here":
		return here(ctx, rest, stdin, stdout)
	case "daemon":
		return daemonCommand(ctx, rest, stdin, stdout)
	}
	if near := nearCommand(command); near != "" {
		return fmt.Errorf("unknown command %q. Did you mean `%s %s`?", command, build.Name, near)
	}
	return fmt.Errorf("unknown command %q. Run `%s help` for all of them", command, build.Name)
}

// nearCommand is the command of the help that is one or two typos
// away from typed, like deploy for deplyo, or "" when none is.
func nearCommand(typed string) string {
	best, bestDistance := "", 3
	for line := range strings.Lines(usage) {
		name, _, ok := usageEntry(strings.TrimRight(line, "\n"))
		if !ok || name == build.Name {
			continue
		}
		command, _, _ := strings.Cut(name, " ")
		if distance := typos(typed, command); distance < bestDistance {
			best, bestDistance = command, distance
		}
	}
	return best
}

// typos is how many letters to add, remove, change or swap to turn a
// into b.
func typos(a, b string) int {
	// row[j] is the distance of the a so far to b[:j].
	before := make([]int, len(b)+1)
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	var earlier []int
	for i := 1; i <= len(a); i++ {
		earlier, before, row = before, row, make([]int, len(b)+1)
		row[0] = i
		for j := 1; j <= len(b); j++ {
			change := 1
			if a[i-1] == b[j-1] {
				change = 0
			}
			row[j] = min(before[j]+1, row[j-1]+1, before[j-1]+change)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				row[j] = min(row[j], earlier[j-2]+1)
			}
		}
	}
	return row[len(b)]
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

// stopSignals stop a run: Ctrl-C, a kill, a closed terminal.
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// errStopped is Ctrl-C while a question waits for its answer.
var errStopped = errors.New("stopped")

// readLine reads one line and trims it. Ctrl-C stops the wait.
func readLine(stdin io.Reader) (string, error) {
	return untilStopped(func() (string, error) {
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimSpace(line), nil
	})
}

// untilStopped waits for read, or for Ctrl-C, which says errStopped. A
// read from a terminal does not end on Ctrl-C by itself. The read left
// waiting ends with the run.
func untilStopped(read func() (string, error)) (string, error) {
	stopped, stop := signal.NotifyContext(context.Background(), stopSignals...)
	defer stop()
	type answer struct {
		text string
		err  error
	}
	answered := make(chan answer, 1)
	go func() {
		text, err := read()
		answered <- answer{text, err}
	}()
	select {
	case got := <-answered:
		return got.text, got.err
	case <-stopped.Done():
		return "", errStopped
	}
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
	flags := newFlags("daemon uninstall", "daemon uninstall [--apps] [-y]").withYes()
	withApps := flags.Bool("apps", false, "remove the apps with their data, the proxy, the tunnel and the firewall too")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	question := "Remove " + build.Name + " from this server? The apps keep running."
	if *withApps {
		question = "Remove " + build.Name + " and every app on this server with its data? This cannot be undone."
	}
	if err := confirm(stdin, stdout, flags.yes(), question); err != nil {
		return err
	}
	kept, err := daemon.Uninstall(ctx, dir, *withApps, func(format string, args ...any) { step(stdout, format, args...) })
	if err != nil {
		return err
	}
	done(stdout, "%s is removed from this server", build.Name)
	if len(kept) > 0 {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("Still here: "+strings.Join(kept, "; ")+"."))
	}
	if !*withApps {
		hint(stdout, "Remove the apps and their data too, as root, with", build.Name+" daemon uninstall --apps")
	}
	return nil
}
