// Package cli is `ops <command>`. Without a command, `ops` opens the TUI.
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

	"github.com/axadrn/ops/internal/agent"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
	"github.com/axadrn/ops/internal/tui"
	"golang.org/x/term"
)

const usage = `ops deploys and runs Docker Compose apps on your own servers.

Usage
  ops                Open the terminal UI
  ops <command>      Run one command

Account
  login              Sign in with a personal API token: login [--url <url>], token asked or on stdin
  logout             Forget the token on this machine
  activity           Who deployed, restored or changed what: activity [--limit 30]
  team               Members and open invitations of your workspace
  team invite        Invite someone: team invite ada@example.com [--admin]

Servers
  servers            List servers
  servers add        Add a server and print its install command: servers add [web-1]
  servers rm         Remove a server: servers rm web-1 [--yes]
  servers update     Update the agent to the control plane's version: servers update [web-1]
  servers secure     Lock it down: secure web-1 [--tunnel on|off] [--ssh tailscale|open] [--firewall on|off]
  servers rules      The server's own rules for ops, from /etc/ops/rules on it
  integrations       Cloudflare token and Tailscale key; set one: integrations cloudflare, key on stdin
  integrations rm    Remove one: integrations rm cloudflare|tailscale [--yes]

Apps
  apps               List apps
  apps add           Add an app: apps add --server <server> <name>
  apps rm            Remove an app; its data stays on the server: apps rm <app> [--yes]

Deploys
  deploy             Deploy this directory: deploy [--dir <dir>] [--server <server>] [--detach] [app]
  deploy --image     Deploy an image built in CI: deploy --image <image> [app]
  deploy --ref       Deploy a branch, tag or commit from GitHub: deploy --ref main
  redeploy           Build the newest source or pull the image again, with the current env
  restart            Start the live release again with the current env and domains: restart [app]
  stop               Take the app offline; its data stays: stop [app] [--yes]
  start              Run a stopped app again: start [app]
  deploys            Deploy history: deploys [app]; one deploy with its log: deploys [app] v3
  rollback           Start an earlier release again: rollback [app] [v<number>]
  logs               Show the last lines of the app: logs [--tail 200] [app]

Domains
  domains            List domains and their DNS: domains [app]
  domains add        Add domains: domains add blog.de www.blog.de
  domains rm         Remove domains: domains rm www.blog.de [--yes]
  domains canonical  Redirect the others to one: domains canonical blog.de|none
  protect            Ask for a password: protect [--user ops], password on stdin
  protect off        Make the app public again: protect off [app]

GitHub
  github             Where the ops GitHub App is installed, and the link to install it
  github repos       Repos ops may read
  link               Follow a repo: link owner/name [--branch main] [--auto=false]
  unlink             Stop following it: unlink [app] [--yes]

CI
  hook               New deploy hook URL for CI: hook [app]
  registry           List registry logins for private images
  registry set       Save a login: registry set <host> <username>
  registry rm        Remove a login: registry rm <host> [--yes]

Data
  db                 Tables, rows and migration of the app's SQLite database: db [app]
  db rows            A page of a table, newest first: db rows orders [--limit 20] [--offset 20]
  db schema          The CREATE statements: db schema [table]
  sql                Run SQL, read only: sql "SELECT * FROM orders LIMIT 5"
  sql --write        Run SQL that changes data, in one transaction

Backups
  backups            Snapshots, point in time window and restore drills of an app: backups [app]
  backup             Take a snapshot now: backup [app]
  restore            Restore a snapshot: restore <snapshot>, or a time: restore --at "2026-10-31 14:02" [--yes]
  backups auto       Snapshot before every change: backups auto on|off
  backups bucket     Show the bucket backups go to
  backups bucket set Set it: --endpoint https://… --bucket … --access-key …, secret key on stdin
  backups bucket rm  Stop backups; the bucket keeps what is in it: backups bucket rm [--yes]

Env
  env                List env vars: env [--show] [app]
  env set            Set env vars: env set [app] KEY=value ...
  env rm             Remove env vars: env rm KEY ... [--yes]
  env pull           Write the env vars to .env
  run                Run a command with the env vars: run -- go run .

Agent
  agent register     Connect this server with an install token
  agent              Run the agent on this server

  version            Show the version
  help               Show this help

Commands that work on an app take it as an argument or from --app, then
from x-ops.app in compose.yml, then from the name of the current directory.
Flags may come before or after the arguments. Commands that remove or
replace something ask first; --yes skips the question, and without a
terminal they need it. Every command shows its usage with --help.
`

// printUsage writes the help: section names bold, descriptions muted on a
// terminal.
func printUsage(w io.Writer) {
	for index, line := range strings.Split(strings.TrimRight(strings.ReplaceAll(usage, "ops", build.Name), "\n"), "\n") {
		name, description, ok := strings.Cut(strings.TrimPrefix(line, "  "), "  ")
		switch {
		case index == 0:
			fmt.Fprintln(w, style.Accent.Bold(true).Render(build.Name)+strings.TrimPrefix(line, build.Name))
		case strings.HasPrefix(line, "  ") && ok:
			fmt.Fprintln(w, "  "+name+"  "+style.Muted.Render(description))
		case line != "" && !strings.Contains(line, " "):
			fmt.Fprintln(w, style.Bold.Render(line))
		default:
			fmt.Fprintln(w, line)
		}
	}
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
		fmt.Fprintln(stdout, build.Name, build.Version)
		return nil
	case "login":
		return login(ctx, rest, stdin, stdout)
	case "logout":
		err := client.RemoveConfig()
		if err != nil {
			return err
		}
		done(stdout, "Logged out")
		return nil
	case "activity":
		return activity(ctx, rest, stdout)
	case "team":
		return team(ctx, rest, stdout)
	case "servers":
		return servers(ctx, rest, stdin, stdout)
	case "apps":
		return apps(ctx, rest, stdin, stdout)
	case "deploy":
		return deploy(ctx, rest, stdout)
	case "deploys":
		return deploys(ctx, rest, stdout)
	case "rollback":
		return rollback(ctx, rest, stdout)
	case "redeploy":
		return redeploy(ctx, rest, stdout)
	case "logs":
		return logs(ctx, rest, stdout)
	case "db":
		return db(ctx, rest, stdout)
	case "sql":
		return sql(ctx, rest, stdin, stdout)
	case "backups":
		return backups(ctx, rest, stdin, stdout)
	case "github":
		return github(ctx, rest, stdout)
	case "integrations":
		return integrations(ctx, rest, stdin, stdout)
	case "link":
		return link(ctx, rest, stdout)
	case "unlink":
		return unlink(ctx, rest, stdin, stdout)
	case "backup":
		return backUp(ctx, rest, stdout)
	case "restore":
		return restore(ctx, rest, stdin, stdout)
	case "env":
		return env(ctx, rest, stdin, stdout)
	case "hook":
		return hook(ctx, rest, stdout)
	case "domains":
		return domains(ctx, rest, stdin, stdout)
	case "protect":
		return protect(ctx, rest, stdin, stdout)
	case "restart", "start":
		return restart(ctx, command, rest, stdout)
	case "stop":
		return stop(ctx, rest, stdin, stdout)
	case "registry":
		return registry(ctx, rest, stdin, stdout)
	case "run":
		return runWithEnv(ctx, rest, stdin, stdout)
	case "agent":
		return agentCommand(ctx, rest, stdout)
	}
	return fmt.Errorf("unknown command %q. Run `%s help` for all of them", command, build.Name)
}

func openTUI(ctx context.Context) error {
	config, err := client.LoadConfig()
	if err != nil {
		return err
	}
	api, err := client.New(config)
	if err != nil {
		return err
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	return tui.Run(ctx, api, config.URL, dir)
}

func login(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	config, err := client.LoadConfig()
	if err != nil {
		return err
	}
	flags := newFlags("login", "login [--url <control plane>] [--token <token>], or the token on stdin: echo \"$TOKEN\" | "+build.Name+" login")
	baseURL := flags.String("url", config.URL, "control plane URL")
	token := flags.String("token", "", "personal API token; without it "+build.Name+" asks for it, or reads it from stdin")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return flags.wrong()
	}
	config.URL = strings.TrimRight(*baseURL, "/")
	config.Token, config.TokenFromEnv = *token, false
	if config.Token == "" {
		if isTerminal(stdin) {
			step(stdout, "Create a token with read and write access under Settings at %s", style.Bold.Render(config.URL+"/app"))
			fmt.Fprint(stdout, style.Muted.Render(style.SymbolStep)+" Paste it here: ")
		}
		config.Token, err = readSecret(stdin)
		if isTerminal(stdin) {
			fmt.Fprintln(stdout)
		}
		if err != nil {
			return err
		}
		if config.Token == "" {
			return fmt.Errorf("no token. Create one under Settings at %s/app and paste it, or pass it with --token", config.URL)
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
	err = client.SaveConfig(config)
	if err != nil {
		return err
	}
	done(stdout, "Logged in as %s %s", style.Bold.Render(account.Email), style.Muted.Render("on "+config.URL))
	if os.Getenv("OPS_TOKEN") != "" {
		hint(stdout, "OPS_TOKEN is set and wins over this login. Use the new one with", "unset OPS_TOKEN")
	}
	return nil
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
	// Help works without a login.
	api, apiErr := client.New(config)
	command := "ls"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	switch command {
	case "ls", "list":
		flags := newFlags("servers", "servers")
		if err := flags.parse(args); err != nil {
			return err
		}
		if apiErr != nil {
			return apiErr
		}
		list, err := api.Servers(ctx)
		if err != nil {
			return err
		}
		meta, _ := api.Meta(ctx)
		PrintServers(stdout, list, meta.Version)
		return nil
	case "add":
		flags := newFlags("servers add", "servers add [name]")
		if err := flags.parse(args); err != nil {
			return err
		}
		if apiErr != nil {
			return apiErr
		}
		// Without a name the control plane picks the next free web-<n>.
		created, err := api.AddServer(ctx, strings.Join(flags.Args(), " "))
		var apiErr *client.Error
		if errors.As(err, &apiErr) && apiErr.Code == "subscription_required" {
			return fmt.Errorf("adding servers needs the ops plan: an owner subscribes under Billing at %s/app", config.URL)
		}
		if err != nil {
			return err
		}
		done(stdout, "Added %s. Run this on the server as root within the next hour:", style.Bold.Render(created.Server.Name))
		// The command stays on one line, so it copies in one piece.
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "  "+style.Accent.Render(created.InstallCommand))
		fmt.Fprintln(stdout)
		hint(stdout, "It shows online in", build.Name+" servers")
		return nil
	case "secure":
		if apiErr != nil {
			return apiErr
		}
		return secure(ctx, api, args, stdout)
	case "rules":
		flags := newFlags("servers rules", "servers rules <server>")
		if err := flags.parse(args); err != nil {
			return err
		}
		if apiErr != nil {
			return apiErr
		}
		if flags.NArg() != 1 {
			return flags.wrong()
		}
		return printRules(ctx, api, flags.Arg(0), stdout)
	case "update":
		if apiErr != nil {
			return apiErr
		}
		return updateAgents(ctx, api, args, stdout)
	case "rm", "remove":
		flags := newFlags("servers rm", "servers rm <server> [--yes]").withYes()
		if err := flags.parse(args); err != nil {
			return err
		}
		if apiErr != nil {
			return apiErr
		}
		if flags.NArg() != 1 {
			return flags.wrong()
		}
		name := flags.Arg(0)
		err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Remove server %s and disconnect its agent?", name))
		if err != nil {
			return err
		}
		err = api.RemoveServer(ctx, name)
		if err != nil {
			return err
		}
		done(stdout, "Removed %s. Its agent is disconnected", style.Bold.Render(name))
		return nil
	}
	return fmt.Errorf("unknown command %q. Run `%s help`", "servers "+command, build.Name)
}

// updateAgents updates the agent of one server, or of every online server
// whose agent is not the control plane's version.
func updateAgents(ctx context.Context, api *client.Client, args []string, stdout io.Writer) error {
	flags := newFlags("servers update", "servers update [server]")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return flags.wrong()
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
		done(stdout, "Every online agent runs %s, like the control plane.", dash(meta.Version))
		return nil
	}
	failed := false
	for _, server := range targets {
		progress := newProgress(stdout, fmt.Sprintf("updating the agent of %s from %s to %s", server.Name, dash(server.AgentVersion), dash(meta.Version)))
		updated, err := api.UpdateAgent(ctx, server.ID)
		progress.end()
		if err != nil {
			printError(stdout, fmt.Errorf("%s: %w", server.Name, err))
			failed = true
			continue
		}
		done(stdout, "%s runs agent %s now", style.Bold.Render(updated.Name), updated.AgentVersion)
	}
	if failed {
		return errReported
	}
	return nil
}

// PrintServers writes the server table. An agent that is not the control
// plane's version gets a note to update it.
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
		agentVersion := dash(server.AgentVersion)
		if server.Outdated(version) {
			agentVersion += " " + style.Warning.Render("update to "+version)
			outdated++
		}
		rows = append(rows, []string{style.Bold.Render(server.Name), badge(stdout, style.Server(server), server.Status()), server.System(), agentVersion, server.LastSeen(now), server.Access(), server.RulesText()})
	}
	table(stdout, []string{"NAME", "STATUS", "SYSTEM", "AGENT", "LAST SEEN", "ACCESS", "RULES"}, rows)
	if outdated > 0 {
		fmt.Fprintln(stdout)
		hint(stdout, fmt.Sprintf("The control plane runs %s. Update the agents with", version), build.Name+" servers update")
	}
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func agentCommand(ctx context.Context, args []string, stdout io.Writer) error {
	dir := agent.StateDir()
	if len(args) > 0 && args[0] == "register" {
		flags := newFlags("agent register", "agent register --url <control plane> --token <install token>")
		baseURL := flags.String("url", client.DefaultURL, "control plane URL")
		token := flags.String("token", "", "install token")
		err := flags.parse(args[1:])
		if err != nil {
			return err
		}
		if *token == "" || flags.NArg() > 0 {
			return flags.wrong()
		}
		state, err := agent.Register(ctx, dir, *baseURL, *token)
		if err != nil {
			return err
		}
		done(stdout, "Registered as server %s. State is in %s", state.ServerID, dir)
		return nil
	}
	if len(args) > 0 {
		return fmt.Errorf("unknown command %q. Run `%s help`", "agent "+args[0], build.Name)
	}
	state, err := agent.LoadState(dir)
	if err != nil {
		return err
	}
	return agent.Run(ctx, dir, state)
}

// printRules shows /etc/ops/rules of a server as its agent reported it.
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
			hint(stdout, "Write them on the server in", "/etc/ops/rules")
			return nil
		}
		fmt.Fprintf(stdout, "/etc/ops/rules on %s:\n\n", style.Bold.Render(server.Name))
		for _, line := range lines {
			fmt.Fprintln(stdout, "  "+line)
		}
		fmt.Fprintln(stdout)
		hint(stdout, "Only the owner of the server changes them, on the server. See", "/docs/guides/security#rules")
		return nil
	}
	return fmt.Errorf("no server %q", nameOrID)
}
