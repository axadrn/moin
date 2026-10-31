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
	"text/tabwriter"
	"time"

	"github.com/axadrn/ops/internal/agent"
	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/tui"
	"golang.org/x/term"
)

const usage = `Usage: ops [command]

Without a command, ops opens the terminal UI.

Commands that work on an app take it from --app, from x-ops.app in
compose.yml, or from the name of the current directory.

Commands:
  login            Sign in with a personal API token
  logout           Forget the token on this machine
  servers          List servers
  servers add      Add a server and print its install command
  servers rm       Remove a server
  apps             List apps
  apps add         Add an app: apps add --server <server> <name>
  apps rm          Remove an app; its data stays on the server
  deploy           Deploy this directory: deploy [--server <server>] [app]
  rollback         Start an earlier release again: rollback [v<number>]
  redeploy         Build the newest source again with the current env
  logs             Show the last lines of the app: logs [--tail 200] [app]
  env              List env vars: env [--show]
  env set          Set env vars: env set KEY=value ...
  env rm           Remove env vars: env rm KEY ...
  env pull         Write the env vars to .env
  run              Run a command with the env vars: run -- go run .
  agent register   Connect this server with an install token
  agent            Run the agent on this server
  version          Show the version
  help             Show this help
`

// Run runs one command and returns the exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := run(ctx, args, stdin, stdout)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", build.Name, err)
		return 1
	}
	return 0
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return openTUI(ctx)
	}
	command, rest := args[0], args[1:]
	switch command {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, strings.ReplaceAll(usage, "ops", build.Name))
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
		fmt.Fprintln(stdout, "Logged out.")
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
	case "env":
		return env(ctx, rest, stdout)
	case "run":
		return runWithEnv(ctx, rest, stdin, stdout)
	case "agent":
		return agentCommand(ctx, rest, stdout)
	}
	return fmt.Errorf("unknown command %q. Run `%s help`", command, build.Name)
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
		fmt.Fprintf(stdout, "Create a token with read and write access at %s/app/settings\n", config.URL)
		fmt.Fprint(stdout, "Paste it here: ")
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
	fmt.Fprintf(stdout, "Logged in as %s.\n", account.Email)
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
		fmt.Fprintf(stdout, "Added %s. Run this on the server within the next hour:\n\n  %s\n\n", created.Server.Name, created.InstallCommand)
		return nil
	case "rm", "remove":
		if len(args) != 2 {
			return fmt.Errorf("usage: %s servers rm <name or id>", build.Name)
		}
		err := api.RemoveServer(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Removed %s.\n", args[1])
		return nil
	}
	return fmt.Errorf("unknown command %q. Run `%s help`", "servers "+args[0], build.Name)
}

// PrintServers writes the server table.
func PrintServers(stdout io.Writer, list []client.Server) {
	if len(list) == 0 {
		fmt.Fprintf(stdout, "No servers yet. Add one with `%s servers add <name>`.\n", build.Name)
		return
	}
	table := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "NAME\tSTATUS\tSYSTEM\tAGENT\tLAST SEEN")
	for _, server := range list {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", server.Name, server.Status(), server.System(), dash(server.AgentVersion), server.LastSeen(time.Now()))
	}
	table.Flush()
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
		fmt.Fprintf(stdout, "Registered as server %s. State is in %s.\n", state.ServerID, dir)
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
