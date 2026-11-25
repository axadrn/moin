// Package cli is `moin <command>`. Without a command, `moin` opens the TUI.
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
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
	"github.com/axadrn/moin/internal/tui"
	"github.com/axadrn/moin/protocol"
	"golang.org/x/term"
)

const usage = `moin: Deploy anything. Lose nothing.
Your apps on your servers. Backups on your schedule, checked every day.

Usage
  moin                Open the terminal UI; without a login it signs you in through the browser first
  moin <command>      Run one command

Account
  login              Connect this computer through the browser, with the rights you pick: login [--url <url>]; --token <token> or a CI token on stdin for CI
  logout             End this computer's connection and forget it, and what the TUI kept on this machine
  connections        Your connections and the CI tokens of the workspace; remove one: connections rm <id> [--yes]
  status             Is everything OK? What needs you, updates, what runs; exits 1 when something needs you
  activity           Who deployed, restored or changed what: activity [--limit 30]
  alerts             Email me when a site or server goes down or a deploy fails, on by default: alerts [on|off]; also lists the destinations
  alerts add         Send alerts to Slack, Discord, Telegram or a webhook: alerts add slack|discord|telegram|webhook <name> [--chat <id>] [--events a,b], URL or bot token on stdin
  alerts test        Send a test alert: alerts test <name>
  alerts rm          Remove a destination: alerts rm <name> [--yes]
  team               Members and open invitations of your workspace
  team invite        Invite someone as member: team invite ada@example.com
  team resend        Send an invitation again: team resend ada@example.com
  team revoke        Take an invitation back: team revoke ada@example.com [--yes]
  team rename        Rename the workspace: team rename "Acme Inc"

Servers
  servers            List servers
  servers add        Add a server and print its install command: servers add [web-1], no name gets the next free web-N
  servers rm         Remove a server: servers rm web-1 [--confirm web-1]
  servers rename     Rename a server: servers rename web-1 web-2
  servers install-command  A new one-time install command, to install or move the daemon: servers install-command web-1
  servers update     Update the daemon to the hub's version now: servers update [web-1]
  servers auto-update  Daemons update by themselves, on by default: servers auto-update [on|off]
  servers secure     Lock it down: servers secure web-1 [--tunnel on|off] [--ssh tailscale|everyone|off] [--firewall on|off] [--yes]
  servers rules      The server's own rules for moin, from /etc/moin/rules on it
  integrations       Cloudflare token and Tailscale key; set one: integrations cloudflare|tailscale [--server web-1], key on stdin
  integrations rm    Remove one: integrations rm cloudflare|tailscale [--server web-1] [--yes]

Apps
  apps               List apps; one app with its domains and deploys: apps <app>
  apps add           Add an app; shows its password once: apps add --server <server> <name>
  apps rename        Rename an app; links, data and backups stay: apps rename my-app blog
  apps rm            Remove an app; its data stays on the server: apps rm <app> [--confirm <app>]
  apps pin           Keep an app above the others that run fine: apps pin [app]; apps unpin [app]

Deploys
  deploy             Deploy the pushed commit of this repo from GitHub: deploy [--server <server>] [--detach] [--yes] [app]; a new app asks first
  deploy --image     Deploy a CI image with the compose file of a commit: deploy --image <image> [--ref <commit>] [app]
  deploy --ref       Deploy a branch, tag or commit from GitHub: deploy --ref main
  redeploy           Build the newest source or pull the image again, with the current env
  restart            Start the live deploy again with the current env and domains: restart [app]
  stop               Take the app offline; its data stays: stop [app] [--yes]
  start              Run a stopped app again: start [app]
  deploys            Deploy history: deploys [app]; one deploy with its log: deploys [app] v3
  rollback           Start an earlier deploy again: rollback [app] [v<number>] [--yes]
  logs               Show the last lines of the app: logs [--tail 200] [app]
  exec               Run a command in a container, no shell: exec [app] [--service web] -- rails db:migrate
  compose            The compose file moin runs, without secret values, to run it elsewhere: compose [app] > compose.yml

Domains
  domains            List domains and their DNS: domains [app]
  domains add        Add domains: domains add my-app.example.com www.my-app.example.com, or for another service: --service api api.my-app.example.com
  domains rm         Remove domains: domains rm www.my-app.example.com [--yes]
  domains canonical  Redirect the others to one: domains canonical my-app.example.com|none
  instant            The instant address, https://<app>.<ip>.sslip.io: instant [app]
  instant <provider> Pick sslip.io or nip.io, or off: instant nip.io [app]
  protect            Every address asks for a password: protect [--user moin] [app], yours on stdin, or none and moin makes one
  protect off        No address asks for a password: protect off [app] [--yes]

Uptime
  uptime             Uptime of every domain that serves, 24 hours, 7 and 30 days, and the last outages: uptime [app]
  uptime path        The path uptime visits on a domain: uptime path my-app.example.com /health, or default
  uptime pause       Pause uptime of a domain: uptime pause my-app.example.com
  uptime resume      Visit it every minute again: uptime resume my-app.example.com

Metrics
  metrics            CPU, memory and network of an app now, with a sparkline: metrics [app] [--range 1h|24h|7d|30d]
  metrics --server   A server: CPU, memory, disk, network and its apps by memory: metrics --server web-1

GitHub
  github             Where the moin GitHub App is installed, and the link to install it
  github repos       Repos moin may read, and where to give it more
  github branches    Branches of a repo: github branches owner/name
  link               Follow a repo; deploy on push turns the deploy hook off: link owner/name [--branch main] [--auto=false] [--yes]
  unlink             Stop following it: unlink [app] [--yes]

CI
  hook               New deploy hook URL for CI; pushes no longer deploy: hook [app] [--yes]
  hook off           Turn the deploy hook off; CI can no longer deploy: hook off [app] [--yes]
  registry           List registry logins for private images, and the servers that keep each
  registry set       Hand a server a login: registry set <host> <username> [--server web-1], password on stdin
  registry rm        Remove a login: registry rm <host> [--yes]

Catalog
  catalog            Open source apps to install in one click, with their newest release
  catalog <app>      An app of the catalog with every release: catalog umami
  catalog install    Install one: catalog install umami --server web-1 [--version 3.4.1] [--name stats] [--domain stats.example.com]

Data
  db                 Tables, rows and migration of the app's database, SQLite or Postgres: db [app]
  db rows            A page of a table, newest first: db rows users [--limit 20] [--offset 20]
  db schema          The CREATE statements: db schema [table]
  sql                Run SQL, read only: sql "SELECT * FROM users LIMIT 5"
  sql --write        Run SQL that changes data, in one transaction; DROP or DELETE asks [--yes]

Backups
  backups            The copies of each database, the moments to go back to, how often: backups [app] [--every 15m|1h|1d] [--keep 1d|3d|7d|30d]
  restore            Go back to a copy; can be undone: restore [app] --to before-v12|14:01 [--confirm <app>]
  backup download    Save a copy of a database from your bucket: backup download [app] [--db <path>] [--to <moment>] [--link]

Buckets
  buckets            Your buckets, the default first, the apps that use each and the servers that keep its key
  buckets add        Add one: buckets add <name> --endpoint https://… --bucket … --access-key … [--default] [--server web-1], secret key on stdin
  buckets edit       Change one: buckets edit <name> [--name …] [--bucket …] [--access-key …] [--secret] [--server web-1]
  buckets key        Hand one more server the key: buckets key <name> --server web-2, secret key on stdin
  buckets default    Make one the default for apps without their own: buckets default <name>
  buckets rm         Remove one nothing uses; what is in it stays: buckets rm <name> [--confirm <name>]
  buckets use        Move an app's backups: buckets use <bucket> [app], or --default

Env
  env                List env vars and when each changed; values are write-only: env [app]
  env set            Set env vars: env set [app] KEY=value ...
  env rm             Remove env vars: env rm KEY ... [--yes]
  env push           Replace all env vars with a .env, after a yes: env push [file] [--yes]

Daemon
  daemon register    Connect this server with an install token
  daemon uninstall   Remove the daemon from this server; the apps keep running: daemon uninstall [--apps] [--yes]
  daemon             Run the daemon on this server

  version            Show the version of moin and the hub, and a newer release
  update             Update a self-hosted hub to the newest release [--yes]
  update <app>       Update an app from the catalog; restore can go back to before it: update umami [--version 3.4.1]
  help               Show this help

Commands that work on an app take it as an argument or from --app, then
the app that follows the GitHub repo of the current directory, then the
directory's name. Flags may come before or after the arguments. Commands
that remove or replace something ask first; --yes skips the question, and without a
terminal they need it. Every command shows its usage with --help.

Docs: https://moin.run/docs, or /docs on your own hub.
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
			fmt.Fprintln(w, style.Accent.Bold(true).Render(build.Name)+strings.TrimPrefix(line, build.Name))
		case ok:
			fmt.Fprintln(w, "  "+name+strings.Repeat(" ", width-len(name)+2)+style.Muted.Render(description))
		case line != "" && !strings.Contains(line, " "):
			fmt.Fprintln(w, style.Bold.Render(line))
		default:
			fmt.Fprintln(w, line)
		}
	}
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
	picked = pickedApp{}
	err := run(ctx, args, stdin, stdout)
	var help helpError
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.As(err, &help):
		help.print(stdout)
		return 0
	case errors.Is(err, errReported):
	case ctx.Err() != nil:
		fmt.Fprintln(stderr)
		printError(stderr, errors.New("stopped"))
	default:
		printError(stderr, err)
	}
	return 1
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return openTUI(ctx)
	}
	command, rest := args[0], args[1:]
	switch command {
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	case "version", "--version":
		return version(ctx, rest, stdout)
	case "update":
		return update(ctx, rest, stdin, stdout)
	case "login":
		return login(ctx, rest, stdin, stdout)
	case "logout":
		if err := noArguments(command, rest); err != nil {
			return err
		}
		return logout(ctx, stdout)
	case "connections":
		return connections(ctx, rest, stdin, stdout)
	case "status":
		return status(ctx, rest, stdout)
	case "activity":
		return activity(ctx, rest, stdout)
	case "alerts":
		return alerts(ctx, rest, stdin, stdout)
	case "team":
		return team(ctx, rest, stdin, stdout)
	case "servers":
		return servers(ctx, rest, stdin, stdout)
	case "apps":
		return apps(ctx, rest, stdin, stdout)
	case "deploy":
		return deploy(ctx, rest, stdin, stdout)
	case "deploys":
		return deploys(ctx, rest, stdout)
	case "rollback":
		return rollback(ctx, rest, stdin, stdout)
	case "redeploy":
		return redeploy(ctx, rest, stdout)
	case "logs":
		return logs(ctx, rest, stdout)
	case "compose":
		return compose(ctx, rest, stdout)
	case "db":
		return db(ctx, rest, stdout)
	case "sql":
		return sql(ctx, rest, stdin, stdout)
	case "backups":
		return backups(ctx, rest, stdout)
	case "buckets":
		return buckets(ctx, rest, stdin, stdout)
	case "github":
		return github(ctx, rest, stdout)
	case "integrations":
		return integrations(ctx, rest, stdin, stdout)
	case "link":
		return link(ctx, rest, stdin, stdout)
	case "unlink":
		return unlink(ctx, rest, stdin, stdout)
	case "backup":
		return backUp(ctx, rest, stdout)
	case "restore":
		return restore(ctx, rest, stdin, stdout)
	case "env":
		return env(ctx, rest, stdin, stdout)
	case "hook":
		return hook(ctx, rest, stdin, stdout)
	case "domains":
		return domains(ctx, rest, stdin, stdout)
	case "instant":
		return instant(ctx, rest, stdout)
	case "protect":
		return protect(ctx, rest, stdin, stdout)
	case "catalog":
		return catalog(ctx, rest, stdout)
	case "uptime":
		return uptime(ctx, rest, stdout)
	case "metrics":
		return metrics(ctx, rest, stdout)
	case "restart", "start":
		return restart(ctx, command, rest, stdout)
	case "stop":
		return stop(ctx, rest, stdin, stdout)
	case "registry":
		return registry(ctx, rest, stdin, stdout)
	case "exec":
		return execCommand(ctx, rest, stdout)
	case "daemon":
		return daemonCommand(ctx, rest, stdin, stdout)
	case "agent":
		// Not a command: the unit of a server from before the daemon runs
		// it once after the update, to move to the daemon.
		return daemon.FromAgent(ctx, daemon.StateDir())
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

func openTUI(ctx context.Context) error {
	config, err := client.LoadConfig()
	if err != nil {
		return err
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	return tui.Run(ctx, tui.Options{Config: config, Dir: dir, Browse: openBrowser,
		Login: func(ctx context.Context, show func(code, link string)) error {
			signedIn, err := client.LoginInBrowser(ctx, config.URL, "tui", func(login client.DeviceLogin) {
				link := login.VerificationURIComplete
				if link == "" {
					link = login.VerificationURI
				}
				show(login.UserCode, link)
			})
			if err != nil {
				return err
			}
			return saveLogin(ctx, signedIn)
		}})
}

// saveLogin saves a new login. When this computer had a connection
// already, it ends now, so logging in again, in the CLI or the TUI, never
// leaves one behind.
func saveLogin(ctx context.Context, config client.Config) error {
	previous, _ := client.LoadSaved()
	if err := client.SaveConfig(config); err != nil {
		return err
	}
	if previous.RefreshToken != "" && previous.RefreshToken != config.RefreshToken {
		_ = client.Logout(ctx, previous)
	}
	return nil
}

func login(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	config, err := client.LoadConfig()
	if err != nil {
		return err
	}
	flags := newFlags("login", "login [--url <hub>] [--web] [--token <token>], or a CI token on stdin: echo \"$TOKEN\" | "+build.Name+" login")
	baseURL := flags.String("url", config.URL, "hub URL")
	token := flags.String("token", "", "CI token, for CI and scripts; without it "+build.Name+" logs in through the browser, or reads the token from stdin")
	web := flags.Bool("web", false, "log in through the browser even without a terminal")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	config.URL = strings.TrimRight(*baseURL, "/")
	// The token must not cross the internet in plain text.
	if err := client.CheckURL(config.URL); err != nil {
		return err
	}
	// A new login replaces all of the old one: a CI token has no refresh
	// token.
	config = client.Config{URL: config.URL, Token: *token}
	switch {
	case config.Token != "":
	case *web || isTerminal(stdin):
		config, err = client.LoginInBrowser(ctx, config.URL, "cli", func(login client.DeviceLogin) {
			showDeviceLogin(stdout, login)
		})
		if err != nil {
			return err
		}
	default:
		config.Token, err = readLine(stdin)
		if err != nil {
			return err
		}
		if config.Token == "" {
			return fmt.Errorf("no token on stdin. Run %s login in a terminal to log in through the browser, or pass a token with --token", build.Name)
		}
	}
	api, err := client.New(config)
	if err != nil {
		return err
	}
	account, err := api.Account(ctx)
	if err != nil {
		return err
	}
	if err := saveLogin(ctx, config); err != nil {
		return err
	}
	who := account.Email
	if account.Name != "" {
		who = account.Name + " (" + account.Email + ")"
	}
	done(stdout, "Logged in as %s %s", style.Bold.Render(who), style.Muted.Render("on "+config.URL))
	if build.Getenv("TOKEN") != "" {
		hint(stdout, client.TokenVariable+" is set and wins over this login. Use the new one with", "unset "+client.TokenVariable)
	}
	return nil
}

// logout ends this computer's connection at the hub, then forgets it and
// what the TUI kept. A CI token stays valid: remove it in Settings.
func logout(ctx context.Context, stdout io.Writer) error {
	saved, err := client.LoadSaved()
	if err != nil {
		return err
	}
	if err := client.Logout(ctx, saved); err != nil {
		step(stdout, "The hub did not answer: %v. Remove the connection in Settings, Connections.", err)
	}
	if err := client.RemoveConfig(); err != nil {
		return err
	}
	if err := tui.ForgetCache(); err != nil {
		return err
	}
	done(stdout, "Logged out")
	if saved.Token != "" && saved.RefreshToken == "" {
		hint(stdout, "The CI token still works. Remove it in the web: Settings, Connections, or with", build.Name+" connections rm <id>")
	}
	return nil
}

// showDeviceLogin prints the code and the page of a login in the browser,
// and opens the page when it can.
func showDeviceLogin(stdout io.Writer, login client.DeviceLogin) {
	step(stdout, "Your code: %s", style.Bold.Render(login.UserCode))
	if openBrowser(login.VerificationURIComplete) == nil {
		step(stdout, "Opened %s in your browser. Check the code, pick the rights and allow it there.", login.VerificationURIComplete)
	} else {
		step(stdout, "Open %s in a browser, check the code, pick the rights and allow it.", style.Bold.Render(login.VerificationURIComplete))
	}
	step(stdout, "Waiting for the browser...")
}

// readSecret reads one line without echo from a terminal, or plainly from a pipe.
func readSecret(stdin io.Reader) (string, error) {
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		secret, err := term.ReadPassword(int(file.Fd()))
		return strings.TrimSpace(string(secret)), err
	}
	return readLine(stdin)
}

// readLine reads one line and trims it.
func readLine(stdin io.Reader) (string, error) {
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func servers(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	config, err := client.LoadConfig()
	if err != nil {
		return err
	}
	command := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	switch command {
	case "":
		return listServers(ctx, config, args, stdout)
	case "add":
		return addServer(ctx, config, args, stdout)
	case "secure":
		return secure(ctx, args, stdin, stdout)
	case "rules":
		return serverRules(ctx, config, args, stdout)
	case "update":
		return updateDaemons(ctx, args, stdout)
	case "auto-update":
		return daemonUpdates(ctx, args, stdout)
	case "install-command":
		return newInstallCommand(ctx, config, args, stdout)
	case "rename":
		return renameServer(ctx, config, args, stdout)
	case "rm":
		return removeServer(ctx, config, args, stdin, stdout)
	}
	return fmt.Errorf("unknown command %q. Run `%s help`", "servers "+command, build.Name)
}

// Each servers command reads its flags before it needs a login, so its help
// works without one.

// listServers is `servers`: the server table.
func listServers(ctx context.Context, config client.Config, args []string, stdout io.Writer) error {
	flags := newFlags("servers", "servers")
	if err := flags.parse(args); err != nil {
		return err
	}
	api, err := client.New(config)
	if err != nil {
		return err
	}
	list, err := api.Servers(ctx)
	if err != nil {
		return err
	}
	meta, _ := api.Meta(ctx)
	PrintServers(stdout, list, meta.Version)
	return nil
}

// addServer is `servers add`: it prints the install command of the new
// server.
func addServer(ctx context.Context, config client.Config, args []string, stdout io.Writer) error {
	flags := newFlags("servers add", "servers add [name]")
	if err := flags.parse(args); err != nil {
		return err
	}
	api, err := client.New(config)
	if err != nil {
		return err
	}
	// Without a name the hub picks the next free web-1, web-2 …
	created, err := api.AddServer(ctx, strings.Join(flags.Args(), " "))
	var apiErr *client.Error
	if errors.As(err, &apiErr) && apiErr.Code == "subscription_required" {
		return fmt.Errorf("adding servers needs the moin plan: an owner subscribes under Billing at %s/app", config.URL)
	}
	if err != nil {
		return err
	}
	done(stdout, "Added %s. Run this on the server as root (or put sudo before sh) within the next hour:", style.Bold.Render(created.Server.Name))
	// The command stays on one line, so it copies in one piece.
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "  "+style.Accent.Render(created.InstallCommand))
	fmt.Fprintln(stdout)
	hint(stdout, "It shows online in", build.Name+" servers")
	return nil
}

// serverRules is `servers rules`: what the owner of a server allows moin.
func serverRules(ctx context.Context, config client.Config, args []string, stdout io.Writer) error {
	flags := newFlags("servers rules", "servers rules <server>")
	if err := flags.parse(args); err != nil {
		return err
	}
	api, err := client.New(config)
	if err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return flags.wrong()
	}
	return printRules(ctx, api, flags.Arg(0), stdout)
}

// newInstallCommand is `servers install-command`: a new one-time install
// command for a server.
func newInstallCommand(ctx context.Context, config client.Config, args []string, stdout io.Writer) error {
	flags := newFlags("servers install-command", "servers install-command <server>")
	if err := flags.parse(args); err != nil {
		return err
	}
	api, err := client.New(config)
	if err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return flags.wrong()
	}
	created, err := api.NewInstallCommand(ctx, flags.Arg(0))
	if err != nil {
		return err
	}
	done(stdout, "New install command for %s. Run this on the server as root (or put sudo before sh) within the next hour:", style.Bold.Render(created.Server.Name))
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "  "+style.Accent.Render(created.InstallCommand))
	fmt.Fprintln(stdout)
	hint(stdout, "It shows once: moin stores only a fingerprint of it. The old command stops working; a daemon that registers with this one takes over. See it in", build.Name+" servers")
	return nil
}

// renameServer is `servers rename`.
func renameServer(ctx context.Context, config client.Config, args []string, stdout io.Writer) error {
	flags := newFlags("servers rename", "servers rename <server> <new name>")
	if err := flags.parse(args); err != nil {
		return err
	}
	api, err := client.New(config)
	if err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return flags.wrong()
	}
	renamed, err := api.RenameServer(ctx, flags.Arg(0), flags.Arg(1))
	if err != nil {
		return err
	}
	done(stdout, "Renamed %s to %s", flags.Arg(0), style.Bold.Render(renamed.Name))
	return nil
}

// removeServer is `servers rm`: it asks for the name, removes the server
// and says how to remove the daemon on it.
func removeServer(ctx context.Context, config client.Config, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("servers rm", "servers rm <server> [--confirm <server>]").withConfirm()
	if err := flags.parse(args); err != nil {
		return err
	}
	api, err := client.New(config)
	if err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return flags.wrong()
	}
	name := flags.Arg(0)
	typed, err := typeName(stdin, stdout, flags.confirmed(), fmt.Sprintf("Remove server %s and disconnect its daemon?", name), name)
	if err != nil {
		return err
	}
	removed, err := api.RemoveServer(ctx, name, typed)
	if err != nil {
		return err
	}
	uninstall := removed.UninstallCommand
	if len(removed.Apps) > 0 {
		done(stdout, "It was offline, so its apps went with it: %s", strings.Join(removed.Apps, ", "))
		fmt.Fprintln(stdout, "  "+style.Muted.Render("They stay on the server if it comes back. Remove them there with "+protocol.UninstallCommand(build.Name)+" --apps."))
	}
	if uninstall == "" {
		done(stdout, "Removed %s. Its daemon never connected, so nothing is installed there", style.Bold.Render(name))
		return nil
	}
	done(stdout, "Removed %s. Its daemon is disconnected", style.Bold.Render(name))
	hint(stdout, "The daemon is still installed there. Remove it on the server as root (or put sudo in front) with", uninstall)
	return nil
}

// updateDaemons updates the daemon of one server, or of every online server
// whose daemon is not the hub's version.
func updateDaemons(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("servers update", "servers update [server]")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	list, err := api.Servers(ctx)
	if err != nil {
		return err
	}
	meta, err := api.Meta(ctx)
	if err != nil {
		return err
	}
	targets := []client.Server{}
	for _, server := range list {
		switch {
		case flags.NArg() == 1:
			if server.Name == flags.Arg(0) || server.ID == flags.Arg(0) {
				targets = append(targets, server)
			}
		case server.Online && server.Outdated(meta.Version):
			targets = append(targets, server)
		}
	}
	if flags.NArg() == 1 && len(targets) == 0 {
		return fmt.Errorf("no server %q", flags.Arg(0))
	}
	if len(targets) == 0 {
		done(stdout, "Every online daemon runs %s, like the hub.", dash(meta.Version))
		return nil
	}
	failed := false
	for _, server := range targets {
		progress := newProgress(stdout, fmt.Sprintf("updating the daemon of %s from %s to %s", server.Name, dash(server.DaemonVersion), dash(meta.Version)))
		updated, err := api.UpdateDaemon(ctx, server.ID)
		progress.end()
		if err != nil {
			printError(stdout, fmt.Errorf("server %s: %w", server.Name, err))
			failed = true
			continue
		}
		done(stdout, "%s runs daemon %s now", style.Bold.Render(updated.Name), updated.DaemonVersion)
	}
	if failed {
		return errReported
	}
	return nil
}

// PrintServers writes the server table. A daemon that is not the hub's
// version gets a note to update it.
func PrintServers(stdout io.Writer, list []client.Server, version string) {
	if len(list) == 0 {
		fmt.Fprintln(stdout, "No servers yet.")
		hint(stdout, "Add one with", build.Name+" servers add web-1")
		return
	}
	now := time.Now()
	rows := make([][]string, 0, len(list))
	outdated := 0
	for _, server := range list {
		daemonVersion := dash(server.DaemonVersion)
		if server.Outdated(version) {
			daemonVersion += " " + style.Warning.Render("update to "+version)
			outdated++
		}
		rows = append(rows, []string{style.Bold.Render(server.Name), badge(stdout, style.Server(server), server.Status()), server.System(), daemonVersion, server.LastSeen(now), server.Access(), server.RulesText()})
	}
	table(stdout, []string{"NAME", "STATUS", "SYSTEM", "DAEMON", "LAST SEEN", "ACCESS", "RULES"}, rows)
	if outdated > 0 {
		fmt.Fprintln(stdout)
		hint(stdout, fmt.Sprintf("The hub runs %s. Update the daemons with", version), build.Name+" servers update")
	}
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func daemonCommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	dir := daemon.StateDir()
	if len(args) > 0 && args[0] == "register" {
		flags := newFlags("daemon register", "daemon register --url <hub> --token <install token>")
		baseURL := flags.String("url", client.DefaultURL, "hub URL")
		token := flags.String("token", "", "install token")
		err := flags.parse(args[1:])
		if err != nil {
			return err
		}
		if *token == "" || flags.NArg() > 0 {
			return flags.wrong()
		}
		state, err := daemon.Register(ctx, dir, *baseURL, *token)
		if err != nil {
			return err
		}
		done(stdout, "Registered as server %s. State is in %s", state.ServerID, dir)
		return nil
	}
	if len(args) > 0 && args[0] == "uninstall" {
		return uninstallDaemon(ctx, dir, args[1:], stdin, stdout)
	}
	flags := newFlags("daemon", "daemon, daemon register --url <hub> --token <install token>, or daemon uninstall [--apps]")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unknown command %q. Run `%s help`", "daemon "+flags.Arg(0), build.Name)
	}
	state, err := daemon.LoadState(dir)
	if err != nil {
		return err
	}
	return daemon.Run(ctx, dir, state)
}

// uninstallDaemon removes the daemon from this server and says what stays.
func uninstallDaemon(ctx context.Context, dir string, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("daemon uninstall", "daemon uninstall [--apps] [--yes]").withYes()
	withApps := flags.Bool("apps", false, "remove the apps with their data, the proxy, the tunnel and the firewall too")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	question := "Remove the daemon from this server? The apps keep running."
	if *withApps {
		question = "Remove the daemon and every app on this server with its data? This cannot be undone."
	}
	if err := confirm(stdin, stdout, flags.yes(), question); err != nil {
		return err
	}
	kept, err := daemon.Uninstall(ctx, dir, *withApps, func(format string, args ...any) { step(stdout, format, args...) })
	if err != nil {
		return err
	}
	done(stdout, "The daemon is removed from this server")
	if len(kept) > 0 {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("Still here: "+strings.Join(kept, "; ")+"."))
	}
	if !*withApps {
		hint(stdout, "Remove the apps and their data too, as root, with", protocol.UninstallCommand(build.Name)+" --apps")
	}
	return nil
}

// printRules shows /etc/moin/rules of a server as its daemon reported it.
func printRules(ctx context.Context, api *client.Client, nameOrID string, stdout io.Writer) error {
	list, err := api.Servers(ctx)
	if err != nil {
		return err
	}
	for _, server := range list {
		if server.Name != nameOrID && server.ID != nameOrID {
			continue
		}
		lines := server.Rules.Lines()
		if len(lines) == 0 {
			fmt.Fprintf(stdout, "%s has no rules: %s may do everything there.\n", style.Bold.Render(server.Name), build.Name)
			hint(stdout, "Write them on the server in", "/etc/moin/rules")
			return nil
		}
		fmt.Fprintf(stdout, "/etc/moin/rules on %s:\n\n", style.Bold.Render(server.Name))
		for _, line := range lines {
			fmt.Fprintln(stdout, "  "+line)
		}
		fmt.Fprintln(stdout)
		hint(stdout, "Only the owner of the server changes them, on the server. See", "/docs/guides/security#rules")
		return nil
	}
	return fmt.Errorf("no server %q", nameOrID)
}
