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
  login              Sign in with a personal API token
  logout             Forget the token on this machine

Servers
  servers            List servers
  servers add        Add a server and print its install command
  servers rm         Remove a server

Apps
  apps               List apps
  apps add           Add an app: apps add --server <server> <name>
  apps rm            Remove an app; its data stays on the server

Deploys
  deploy             Deploy this directory: deploy [--server <server>] [app]
  deploy --image     Deploy an image built in CI: deploy --image <image> [app]
  redeploy           Run the newest source or image again with the current env
  rollback           Start an earlier release again: rollback [v<number>]
  logs               Show the last lines of the app: logs [--tail 200] [app]

Domains
  domains            List domains and their DNS: domains [app]
  domains add        Add domains: domains add blog.de www.blog.de
  domains rm         Remove domains: domains rm www.blog.de
  domains canonical  Redirect the others to one: domains canonical blog.de|none
  protect            Ask for a password: protect [--user ops], password on stdin
  protect off        Make the app public again
  apply              Restart the running release with the current env and domains

CI
  hook               New deploy hook URL for CI: hook [app]
  registry           List registry logins for private images
  registry set       Save a login: registry set <host> <username>
  registry rm        Remove a login: registry rm <host>

Data
  db                 Tables, rows and migration of the app's SQLite database
  db rows            A page of a table, newest first: db rows orders [--limit 20]
  db schema          The CREATE statements: db schema [table]
  sql                Run SQL, read only: sql "SELECT * FROM orders LIMIT 5"
  sql --write        Run SQL that changes data, in one transaction

Backups
  backups            Snapshots, point in time window and restore drills of an app
  backup             Take a snapshot now: backup [app]
  restore            Restore a snapshot: restore <snapshot>, or a time: restore --at "2026-10-31 14:02"
  backups auto       Snapshot before every change: backups auto on|off
  backups bucket     Show the bucket backups go to
  backups bucket set Set it: --endpoint https://… --bucket … --access-key …, secret key on stdin
  backups bucket rm  Stop backups; the bucket keeps what is in it

Env
  env                List env vars: env [--show]
  env set            Set env vars: env set KEY=value ...
  env rm             Remove env vars: env rm KEY ...
  env pull           Write the env vars to .env
  run                Run a command with the env vars: run -- go run .

Agent
  agent register     Connect this server with an install token
  agent              Run the agent on this server

  version            Show the version
  help               Show this help

Commands that work on an app take it from --app, from x-ops.app in
compose.yml, or from the name of the current directory.
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
	err := run(ctx, args, stdin, stdout)
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
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
	case "servers":
		return servers(ctx, rest, stdout)
	case "apps":
		return apps(ctx, rest, stdout)
	case "deploy":
		return deploy(ctx, rest, stdout)
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
	case "backup":
		return backUp(ctx, rest, stdout)
	case "restore":
		return restore(ctx, rest, stdout)
	case "env":
		return env(ctx, rest, stdout)
	case "hook":
		return hook(ctx, rest, stdout)
	case "domains":
		return domains(ctx, rest, stdout)
	case "protect":
		return protect(ctx, rest, stdin, stdout)
	case "apply":
		return apply(ctx, rest, stdout)
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
	flags := flag.NewFlagSet("login", flag.ContinueOnError)
	baseURL := flags.String("url", "", "control plane URL")
	token := flags.String("token", "", "personal API token (default: asks)")
	err := flags.Parse(args)
	if err != nil {
		return err
	}
	config, err := client.LoadConfig()
	if err != nil {
		return err
	}
	if *baseURL != "" {
		config.URL = strings.TrimRight(*baseURL, "/")
	}
	config.Token = *token
	if config.Token == "" {
		step(stdout, "Create a token with read and write access at %s", style.Bold.Render(config.URL+"/app/settings"))
		fmt.Fprint(stdout, style.Muted.Render(style.SymbolStep)+" Paste it here: ")
		config.Token, err = readSecret(stdin)
		fmt.Fprintln(stdout)
		if err != nil {
			return err
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
	return nil
}

// readSecret reads one line without echo from a terminal, or plainly from a pipe.
func readSecret(stdin io.Reader) (string, error) {
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		secret, err := term.ReadPassword(int(file.Fd()))
		return strings.TrimSpace(string(secret)), err
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func servers(ctx context.Context, args []string, stdout io.Writer) error {
	config, err := client.LoadConfig()
	if err != nil {
		return err
	}
	api, err := client.New(config)
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "ls" || args[0] == "list" {
		list, err := api.Servers(ctx)
		if err != nil {
			return err
		}
		PrintServers(stdout, list)
		return nil
	}
	switch args[0] {
	case "add":
		name := strings.Join(args[1:], " ")
		created, err := api.AddServer(ctx, name)
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
	case "rm", "remove":
		if len(args) != 2 {
			return fmt.Errorf("usage: %s servers rm <name or id>", build.Name)
		}
		err := api.RemoveServer(ctx, args[1])
		if err != nil {
			return err
		}
		done(stdout, "Removed %s. Its agent is disconnected", style.Bold.Render(args[1]))
		return nil
	}
	return fmt.Errorf("unknown command %q. Run `%s help`", "servers "+args[0], build.Name)
}

// PrintServers writes the server table.
func PrintServers(stdout io.Writer, list []client.Server) {
	if len(list) == 0 {
		fmt.Fprintln(stdout, "No servers yet.")
		hint(stdout, "Add one with", build.Name+" servers add <name>")
		return
	}
	now := time.Now()
	rows := make([][]string, 0, len(list))
	for _, server := range list {
		rows = append(rows, []string{style.Bold.Render(server.Name), badge(stdout, style.Server(server), server.Status()), server.System(), dash(server.AgentVersion), server.LastSeen(now)})
	}
	table(stdout, []string{"NAME", "STATUS", "SYSTEM", "AGENT", "LAST SEEN"}, rows)
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
		flags := flag.NewFlagSet("agent register", flag.ContinueOnError)
		baseURL := flags.String("url", client.DefaultURL, "control plane URL")
		token := flags.String("token", "", "install token")
		err := flags.Parse(args[1:])
		if err != nil {
			return err
		}
		if *token == "" {
			return errors.New("--token is required")
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
