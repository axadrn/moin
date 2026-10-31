package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
)

func apiClient() (*client.Client, error) {
	config, err := client.LoadConfig()
	if err != nil {
		return nil, err
	}
	return client.New(config)
}

func appName(flagValue, dir string) string { return client.AppName(flagValue, dir) }

func apps(ctx context.Context, args []string, stdout io.Writer) error {
	api, err := apiClient()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "ls" || args[0] == "list" {
		list, err := api.Apps(ctx)
		if err != nil {
			return err
		}
		PrintApps(stdout, list)
		return nil
	}
	switch args[0] {
	case "add":
		flags := flag.NewFlagSet("apps add", flag.ContinueOnError)
		server := flags.String("server", "", "server name or ID")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 1 || *server == "" {
			return fmt.Errorf("usage: %s apps add --server <server> <name>", build.Name)
		}
		app, err := api.AddApp(ctx, flags.Arg(0), *server)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Added %s on %s. Deploy it with `%s deploy %s`.\n", app.Name, app.ServerName, build.Name, app.Name)
		return nil
	case "rm", "remove":
		if len(args) != 2 {
			return fmt.Errorf("usage: %s apps rm <name>", build.Name)
		}
		err := api.RemoveApp(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Removed %s. Its data stays on the server.\n", args[1])
		return nil
	}
	return fmt.Errorf("unknown command %q. Run `%s help`", "apps "+args[0], build.Name)
}

// PrintApps writes the app table.
func PrintApps(stdout io.Writer, list []client.App) {
	if len(list) == 0 {
		fmt.Fprintf(stdout, "No apps yet. Run `%s deploy` in a repo.\n", build.Name)
		return
	}
	table := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(table, "NAME\tSERVER\tSTATUS")
	for _, app := range list {
		fmt.Fprintf(table, "%s\t%s\t%s\n", app.Name, app.ServerName, app.Status())
	}
	table.Flush()
}

// deploy packs the directory, uploads it and follows the deploy. An app that
// does not exist yet is created on the server x-ops or --server names, or on
// the only server.
func deploy(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("deploy", flag.ContinueOnError)
	dir := flags.String("dir", ".", "directory to deploy")
	server := flags.String("server", "", "server for a new app")
	detach := flags.Bool("detach", false, "start the deploy and do not wait for it")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("usage: %s deploy [--dir <dir>] [--server <server>] [app]", build.Name)
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	name := appName(flags.Arg(0), *dir)
	if name == "" {
		return errors.New("cannot tell the app name: pass it, or set x-ops.app in compose.yml")
	}
	_, err = api.App(ctx, name)
	if client.IsNotFound(err) {
		err = createApp(ctx, api, name, *server, *dir, stdout)
	}
	if err != nil {
		return err
	}

	var source bytes.Buffer
	files, err := client.Pack(*dir, &source)
	if err != nil {
		return fmt.Errorf("pack %s: %w", *dir, err)
	}
	fmt.Fprintf(stdout, "Uploading %d files (%s) for %s\n", files, size(source.Len()), name)
	started, err := api.Deploy(ctx, name, &source)
	if err != nil {
		return err
	}
	return follow(ctx, api, name, started, *detach, stdout)
}

func createApp(ctx context.Context, api *client.Client, name, server, dir string, stdout io.Writer) error {
	if server == "" {
		server = client.ReadLocalConfig(dir).Server
	}
	if server == "" {
		servers, err := api.Servers(ctx)
		if err != nil {
			return err
		}
		if len(servers) != 1 {
			return fmt.Errorf("app %s does not exist yet: pass --server or set x-ops.server, you have %d servers", name, len(servers))
		}
		server = servers[0].Name
	}
	app, err := api.AddApp(ctx, name, server)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Created app %s on %s\n", app.Name, app.ServerName)
	return nil
}

// follow prints the deploy log and fails when the deploy fails.
func follow(ctx context.Context, api *client.Client, name string, started client.Deploy, detach bool, stdout io.Writer) error {
	fmt.Fprintf(stdout, "Deploy v%d of %s started\n", started.Number, name)
	if detach {
		return nil
	}
	done, err := api.FollowDeploy(ctx, name, started.Number, func(line string) {
		fmt.Fprintln(stdout, "  "+line)
	})
	if err != nil {
		return err
	}
	if done.Status != "succeeded" {
		return fmt.Errorf("deploy v%d failed: %s", done.Number, done.Error)
	}
	if detected := done.Detected; detected != nil {
		fmt.Fprintf(stdout, "v%d of %s is live: %s, service %s, port %d\n", done.Number, name, detected.Kind, detected.Service, detected.Port)
		for _, database := range detected.Databases {
			fmt.Fprintf(stdout, "Database %s on the server at %s\n", database.Path, database.HostPath)
		}
		return nil
	}
	fmt.Fprintf(stdout, "v%d of %s is live\n", done.Number, name)
	return nil
}

func rollback(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("rollback", flag.ContinueOnError)
	appFlag := flags.String("app", "", "app name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	number := 0
	if flags.NArg() > 1 {
		return fmt.Errorf("usage: %s rollback [--app <app>] [v<number>]", build.Name)
	}
	if flags.NArg() == 1 {
		parsed, err := strconv.Atoi(strings.TrimPrefix(flags.Arg(0), "v"))
		if err != nil || parsed < 1 {
			return fmt.Errorf("%q is not a deploy number like v3", flags.Arg(0))
		}
		number = parsed
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	name := appName(*appFlag, ".")
	started, err := api.Rollback(ctx, name, number)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Rolling back %s to v%d\n", name, started.FromNumber)
	return follow(ctx, api, name, started, false, stdout)
}

func redeploy(ctx context.Context, args []string, stdout io.Writer) error {
	api, err := apiClient()
	if err != nil {
		return err
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: %s redeploy [app]", build.Name)
	}
	name := appName(strings.Join(args, ""), ".")
	started, err := api.Redeploy(ctx, name)
	if err != nil {
		return err
	}
	return follow(ctx, api, name, started, false, stdout)
}

func logs(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	tail := flags.Int("tail", 200, "lines per container")
	if err := flags.Parse(args); err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	name := appName(flags.Arg(0), ".")
	lines, err := api.Logs(ctx, name, *tail)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		fmt.Fprintf(stdout, "%s has written no log lines yet.\n", name)
	}
	for _, line := range lines {
		fmt.Fprintln(stdout, line)
	}
	return nil
}

// env manages the env vars of an app: ls, set, rm and pull.
func env(ctx context.Context, args []string, stdout io.Writer) error {
	command := "ls"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("env "+command, flag.ContinueOnError)
	appFlag := flags.String("app", "", "app name")
	show := flags.Bool("show", false, "show the values")
	if err := flags.Parse(args); err != nil {
		return err
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	name := appName(*appFlag, ".")
	switch command {
	case "ls", "list":
		values, err := api.Env(ctx, name)
		if err != nil {
			return err
		}
		if len(values) == 0 {
			fmt.Fprintf(stdout, "%s has no env vars. Set one with `%s env set KEY=value`.\n", name, build.Name)
			return nil
		}
		for _, key := range sortedKeys(values) {
			value := "••••••"
			if *show {
				value = values[key]
			}
			fmt.Fprintf(stdout, "%s=%s\n", key, value)
		}
		return nil
	case "set":
		set := map[string]string{}
		for _, pair := range flags.Args() {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				return fmt.Errorf("%q is not KEY=value", pair)
			}
			set[key] = value
		}
		if len(set) == 0 {
			return fmt.Errorf("usage: %s env set [--app <app>] KEY=value ...", build.Name)
		}
		_, err := api.SetEnv(ctx, name, set, nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Set %s on %s. The next deploy gets them.\n", strings.Join(sortedKeys(set), ", "), name)
		return nil
	case "rm", "remove", "unset":
		if flags.NArg() == 0 {
			return fmt.Errorf("usage: %s env rm [--app <app>] KEY ...", build.Name)
		}
		_, err := api.SetEnv(ctx, name, nil, flags.Args())
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Removed %s from %s. The next deploy drops them.\n", strings.Join(flags.Args(), ", "), name)
		return nil
	case "pull":
		path := ".env"
		if flags.NArg() == 1 {
			path = flags.Arg(0)
		}
		values, err := api.Env(ctx, name)
		if err != nil {
			return err
		}
		var out strings.Builder
		for _, key := range sortedKeys(values) {
			fmt.Fprintf(&out, "%s=%s\n", key, quoteEnv(values[key]))
		}
		err = os.WriteFile(path, []byte(out.String()), 0o600)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Wrote %d env vars of %s to %s\n", len(values), name, path)
		return nil
	}
	return fmt.Errorf("unknown command %q. Run `%s help`", "env "+command, build.Name)
}

// runWithEnv starts a local command with the env vars of an app, without a file.
func runWithEnv(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	appFlag := flags.String("app", "", "app name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() == 0 {
		return fmt.Errorf("usage: %s run [--app <app>] -- <command> [args]", build.Name)
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	values, err := api.Env(ctx, appName(*appFlag, "."))
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, flags.Arg(0), flags.Args()[1:]...)
	command.Stdin, command.Stdout, command.Stderr = stdin, stdout, os.Stderr
	command.Env = os.Environ()
	for _, key := range sortedKeys(values) {
		command.Env = append(command.Env, key+"="+values[key])
	}
	err = command.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	return err
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// quoteEnv quotes a value for a .env file when it needs it.
func quoteEnv(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n\"'\\$#") {
		return value
	}
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "$", `\$`)
	return `"` + replacer.Replace(value) + `"`
}

func size(bytes int) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/(1<<10))
	}
	return fmt.Sprintf("%d B", bytes)
}
