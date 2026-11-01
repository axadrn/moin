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

	"github.com/axadrn/ops/internal/build"
	"github.com/axadrn/ops/internal/client"
	"github.com/axadrn/ops/internal/style"
	"golang.org/x/term"
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
		done(stdout, "Added %s on %s", style.Bold.Render(app.Name), app.ServerName)
		hint(stdout, "Deploy it with", build.Name+" deploy "+app.Name)
		return nil
	case "rm", "remove":
		if len(args) != 2 {
			return fmt.Errorf("usage: %s apps rm <name>", build.Name)
		}
		err := api.RemoveApp(ctx, args[1])
		if err != nil {
			return err
		}
		done(stdout, "Removed %s. Its data stays on the server", style.Bold.Render(args[1]))
		return nil
	}
	return fmt.Errorf("unknown command %q. Run `%s help`", "apps "+args[0], build.Name)
}

// PrintApps writes the app table.
func PrintApps(stdout io.Writer, list []client.App) {
	if len(list) == 0 {
		fmt.Fprintln(stdout, "No apps yet.")
		hint(stdout, "Deploy one from its repo with", build.Name+" deploy")
		return
	}
	rows := make([][]string, 0, len(list))
	for _, app := range list {
		rows = append(rows, []string{style.Bold.Render(app.Name), app.ServerName, badge(stdout, style.App(app), app.Status())})
	}
	table(stdout, []string{"NAME", "SERVER", "STATUS"}, rows)
}

// deploy packs the directory, uploads it and follows the deploy. An app that
// does not exist yet is created on the server x-ops or --server names, or on
// the only server.
func deploy(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("deploy", flag.ContinueOnError)
	dir := flags.String("dir", ".", "directory to deploy")
	server := flags.String("server", "", "server for a new app")
	detach := flags.Bool("detach", false, "start the deploy and do not wait for it")
	image := flags.String("image", "", "deploy this image, built elsewhere, as the main service")
	ref := flags.String("ref", "", "deploy this branch, tag or commit of the linked GitHub repo")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return fmt.Errorf("usage: %s deploy [--dir <dir>] [--server <server>] [--image <image>] [--ref <ref>] [app]", build.Name)
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	name := appName(flags.Arg(0), *dir)
	if name == "" {
		return errors.New("cannot tell the app name: pass it, or set x-ops.app in compose.yml")
	}
	if *ref != "" {
		// The control plane downloads the commit from GitHub; nothing is
		// uploaded.
		step(stdout, "Deploying %s from GitHub", *ref)
		started, err := api.DeployRef(ctx, name, *ref)
		if err != nil {
			return err
		}
		if started.Commit != "" {
			step(stdout, "commit %s %s", started.Commit[:min(7, len(started.Commit))], style.Muted.Render(started.CommitMessage))
		}
		return follow(ctx, api, name, started, "Deploy", *detach, stdout)
	}
	if *image != "" {
		// The server keeps the compose file of the running release and pulls
		// the image; nothing is uploaded.
		step(stdout, "Deploying %s", *image)
		started, err := api.DeployImage(ctx, name, *image)
		if err != nil {
			return err
		}
		return follow(ctx, api, name, started, "Deploy", *detach, stdout)
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
	noun := "files"
	if files == 1 {
		noun = "file"
	}
	step(stdout, "Uploading %d %s to your bucket %s", files, noun, style.Muted.Render("("+size(source.Len())+")"))
	started, err := api.Deploy(ctx, name, source.Bytes())
	var apiErr *client.Error
	if errors.As(err, &apiErr) && apiErr.Code == "no_bucket" {
		return fmt.Errorf("%s deploy puts your code into your own bucket, never on ours: add one with %s backups bucket set, or deploy from GitHub with %s deploy --ref main", build.Name, build.Name, build.Name)
	}
	if err != nil {
		return err
	}
	return follow(ctx, api, name, started, "Deploy", *detach, stdout)
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
	done(stdout, "Created app %s on %s", style.Bold.Render(app.Name), app.ServerName)
	return nil
}

// follow prints the deploy log and fails when the deploy fails. what is
// "Deploy", "Redeploy" or "Rollback".
func follow(ctx context.Context, api *client.Client, name string, started client.Deploy, what string, detach bool, stdout io.Writer) error {
	to := ""
	if started.FromNumber > 0 {
		to = fmt.Sprintf(" to v%d", started.FromNumber)
	}
	step(stdout, "%s v%d of %s%s started", what, started.Number, style.Bold.Render(name), to)
	if detach {
		hint(stdout, "It goes on on the server. Watch it with", build.Name+" apps")
		return nil
	}
	progress := newProgress(stdout, "waiting for the server")
	result, err := api.FollowDeploy(ctx, name, started.Number, func(line string) {
		// The result line below says the same.
		if isResult(line) {
			return
		}
		progress.line(line)
	})
	progress.end()
	switch {
	case ctx.Err() != nil:
		fmt.Fprintln(stdout)
		hint(stdout, "Stopped watching. The deploy goes on on the server. See it with", build.Name+" apps")
		return errReported
	case err != nil:
		return err
	case result.Status != "succeeded":
		fmt.Fprintln(stdout, style.Danger.Render(fmt.Sprintf("%s v%d of %s failed: %s", style.SymbolFailed, result.Number, name, result.Error)))
		if app, err := api.App(ctx, name); err == nil && app.App.LiveNumber > 0 {
			hint(stdout, fmt.Sprintf("v%d stays live. Its logs:", app.App.LiveNumber), build.Name+" logs "+name)
		} else {
			hint(stdout, "Fix it and deploy again with", build.Name+" deploy")
		}
		return errReported
	}
	fmt.Fprintln(stdout, style.Success.Render(style.SymbolDone)+" "+style.Bold.Render(fmt.Sprintf("v%d of %s is live", result.Number, name))+style.Muted.Render(" in "+progress.elapsed()))
	hint(stdout, "See it run with", build.Name+" logs "+name)
	return nil
}

// isResult reports whether a deploy log line is the agent's last word,
// "v4 is live" or "deploy failed: …".
func isResult(line string) bool {
	if strings.HasPrefix(line, "deploy failed: ") {
		return true
	}
	number, ok := strings.CutSuffix(line, " is live")
	_, err := strconv.Atoi(strings.TrimPrefix(number, "v"))
	return ok && strings.HasPrefix(number, "v") && err == nil
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
	return follow(ctx, api, name, started, "Rollback", false, stdout)
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
	return follow(ctx, api, name, started, "Redeploy", false, stdout)
}

// hook gives the app a new deploy hook URL for CI.
func hook(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: %s hook [app]", build.Name)
	}
	api, err := apiClient()
	if err != nil {
		return err
	}
	name := appName(strings.Join(args, ""), ".")
	url, err := api.RotateDeployHook(ctx, name)
	if err != nil {
		return err
	}
	done(stdout, "New deploy hook for %s. It is shown once; the old one stops working:", name)
	fmt.Fprintf(stdout, "\n  %s\n\n", url)
	hint(stdout, "Store it as a CI secret and call it after pushing the image:", `curl -fsS -X POST "$OPS_DEPLOY_HOOK?image=ghcr.io/me/`+name+`:$GITHUB_SHA"`)
	return nil
}

// registry manages the logins agents use to pull private images.
func registry(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	api, err := apiClient()
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "ls" || args[0] == "list" {
		list, err := api.Registries(ctx)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			hint(stdout, "No registry logins. Images from public registries pull without one; for private ones:", build.Name+" registry set ghcr.io <user>")
			return nil
		}
		rows := make([][]string, 0, len(list))
		for _, login := range list {
			rows = append(rows, []string{login.Host, login.Username, login.CreatedAt.Local().Format("Jan 2 2006")})
		}
		table(stdout, []string{"HOST", "USERNAME", "ADDED"}, rows)
		return nil
	}
	switch args[0] {
	case "set", "add":
		if len(args) != 3 {
			return fmt.Errorf("usage: %s registry set <host> <username>, then the password or token on stdin", build.Name)
		}
		if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
			fmt.Fprintf(stdout, "Password or token for %s on %s: ", args[2], args[1])
		}
		password, err := readSecret(stdin)
		if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
			fmt.Fprintln(stdout)
		}
		if err != nil {
			return err
		}
		err = api.SetRegistry(ctx, args[1], args[2], password)
		if err != nil {
			return err
		}
		done(stdout, "Saved the login for %s. Image deploys from it pull with it.", args[1])
		return nil
	case "rm", "remove":
		if len(args) != 2 {
			return fmt.Errorf("usage: %s registry rm <host>", build.Name)
		}
		err := api.RemoveRegistry(ctx, args[1])
		if err != nil {
			return err
		}
		done(stdout, "Removed the login for %s.", args[1])
		return nil
	}
	return fmt.Errorf("unknown command %q. Run `%s help`", "registry "+args[0], build.Name)
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
		fmt.Fprintln(stdout, logLine(line))
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
			fmt.Fprintf(stdout, "%s has no env vars.\n", name)
			hint(stdout, "Set one with", build.Name+" env set KEY=value")
			return nil
		}
		for _, key := range sortedKeys(values) {
			value := style.Muted.Render("••••••")
			if *show {
				value = values[key]
			}
			fmt.Fprintln(stdout, key+style.Muted.Render("=")+value)
		}
		if !*show && isTTY(stdout) {
			hint(stdout, "Values hidden. Show them with", build.Name+" env --show")
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
		done(stdout, "Set %s on %s", strings.Join(sortedKeys(set), ", "), style.Bold.Render(name))
		hint(stdout, "The next deploy gets them. Now:", build.Name+" redeploy "+name)
		return nil
	case "rm", "remove", "unset":
		if flags.NArg() == 0 {
			return fmt.Errorf("usage: %s env rm [--app <app>] KEY ...", build.Name)
		}
		_, err := api.SetEnv(ctx, name, nil, flags.Args())
		if err != nil {
			return err
		}
		done(stdout, "Removed %s from %s", strings.Join(flags.Args(), ", "), style.Bold.Render(name))
		hint(stdout, "The next deploy drops them. Now:", build.Name+" redeploy "+name)
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
		done(stdout, "Wrote %d env vars of %s to %s", len(values), style.Bold.Render(name), path)
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

// logLine mutes the container prefix of a log line, "web-1  | ".
func logLine(line string) string {
	prefix, rest, ok := strings.Cut(line, " | ")
	if !ok || strings.TrimSpace(prefix) == "" || strings.Contains(strings.TrimSpace(prefix), " ") {
		return line
	}
	return style.Muted.Render(prefix+" |") + " " + rest
}
