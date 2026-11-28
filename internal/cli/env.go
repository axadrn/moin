package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/dotenv"
	"github.com/axadrn/moin/internal/style"
)

// env is `moin env [app]`, the names of the app's env vars, `moin env
// edit [app]`, `moin env push [app] [file|-]` and `moin env set [app]
// <NAME>`. A value is never printed: it only shows in your editor. All
// of them work before the app's first deploy, so it starts with its env.
func env(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "edit":
			return envEdit(ctx, args[1:], stdin, stdout)
		case "push":
			return envPush(ctx, args[1:], stdin, stdout)
		case "set":
			return envSet(ctx, args[1:], stdin, stdout)
		}
	}
	flags := newFlags("env", "env [app] [-s server] [--json], or env edit|push|set [app]").withServer().withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	server, err := envServer(ctx, flags, app, stdin)
	if err != nil {
		return err
	}
	var names []string
	if err := sshJSON(ctx, server, []string{"env", app}, nil, &names); err != nil {
		return err
	}
	if flags.json() {
		return printJSON(stdout, names)
	}
	if len(names) == 0 {
		fmt.Fprintln(stdout, app+" has no env vars.")
	}
	for _, name := range names {
		fmt.Fprintln(stdout, name)
	}
	hint(stdout, "Change them with", build.Name+" env edit "+app)
	return nil
}

// envServer is the server of the app's env: the one that runs it, or
// before its first deploy the one -s names or you pick.
func envServer(ctx context.Context, flags *flagSet, app string, stdin io.Reader) (string, error) {
	return appServer(ctx, flags, app, stdin, fmt.Errorf("no server runs %s yet. Name the one it goes to with -s, like -s my-server", app))
}

// envEdit opens $EDITOR with the app's env, values and all, then writes
// what you saved back to the server and restarts the app with it.
func envEdit(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("env edit", "env edit [app] [-s server]").withServer()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	if !isTerminal(stdin) {
		return fmt.Errorf("env edit needs a terminal: it opens your $EDITOR. In a script: `%s env push %s .env`", build.Name, app)
	}
	server, err := envServer(ctx, flags, app, stdin)
	if err != nil {
		return err
	}
	return editEnv(ctx, server, app, stdin, stdout)
}

// editEnv fetches the env from server, lets you edit it, and saves it
// there when it changed.
func editEnv(ctx context.Context, server, app string, stdin io.Reader, stdout io.Writer) error {
	before, err := sshOutput(ctx, server, hereLine("env", "get", app), nil)
	if err != nil {
		return err
	}
	after, err := editUntilValid(ctx, app, before, stdin, stdout)
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		fmt.Fprintln(stdout, style.Muted.Render("Nothing changed."))
		return nil
	}
	var saved daemon.EnvSaved
	if err := sshJSON(ctx, server, []string{"env", "set", app}, bytes.NewReader(after), &saved); err != nil {
		return err
	}
	return startWithEnv(ctx, server, app, saved, stdout)
}

// envPush is `moin env push [app] [file|-]`: the app's env becomes the
// file, .env by default, or what comes on stdin with -. It shows what
// changes, by name, and asks first.
func envPush(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("env push", "env push [app] [file|-] [-s server] [-y]").withServer().withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, file := flags.Arg(0), ".env"
	var err error
	switch flags.NArg() {
	case 0:
		app, err = folderApp()
	case 1:
		err = checkApp(app)
	case 2:
		file, err = flags.Arg(1), checkApp(app)
	default:
		err = flags.wrong()
	}
	if err != nil {
		return err
	}
	text, err := readEnvFile(file, stdin)
	if err != nil {
		return err
	}
	next, err := dotenv.Parse(string(text))
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	server, err := envServer(ctx, flags, app, stdin)
	if err != nil {
		return err
	}
	before, err := sshOutput(ctx, server, hereLine("env", "get", app), nil)
	if err != nil {
		return err
	}
	old, err := dotenv.Parse(string(before))
	if err != nil {
		return err
	}
	change := dotenv.Diff(old, next)
	if change.None() {
		fmt.Fprintln(stdout, style.Muted.Render("Nothing changed."))
		return nil
	}
	printChange(stdout, change)
	if err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Change the env of %s on %s?", app, server)); err != nil {
		return err
	}
	var saved daemon.EnvSaved
	if err := sshJSON(ctx, server, []string{"env", "set", app}, bytes.NewReader(text), &saved); err != nil {
		return err
	}
	return startWithEnv(ctx, server, app, saved, stdout)
}

// readEnvFile reads the .env text of env push: the file, or stdin for -.
func readEnvFile(file string, stdin io.Reader) ([]byte, error) {
	if file == "-" {
		return io.ReadAll(io.LimitReader(stdin, 1<<20))
	}
	return os.ReadFile(file)
}

// printChange lists what a change of the env does, by name: + added,
// ~ replaced, - removed.
func printChange(stdout io.Writer, change dotenv.Change) {
	for _, name := range change.Added {
		fmt.Fprintln(stdout, "  "+style.Success.Render("+")+" "+name)
	}
	for _, name := range change.Replaced {
		fmt.Fprintln(stdout, "  "+style.Warning.Render("~")+" "+name)
	}
	for _, name := range change.Removed {
		fmt.Fprintln(stdout, "  "+style.Danger.Render("-")+" "+name)
	}
}

// envSet is `moin env set [app] <NAME>`: one env var. Its value is typed
// at a prompt that does not show it, or comes on stdin. It is never an
// argument, where the shell history and ps would see it.
func envSet(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("env set", "env set [app] <NAME> [-s server]").withServer()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, rest, err := appArgs(flags, 1)
	if err != nil {
		return err
	}
	name := rest[0]
	if strings.Contains(name, "=") {
		return fmt.Errorf("the value never goes on the command line, where your shell history keeps it. Type it when %s asks, or pipe it in: `echo \"$VALUE\" | %s env set %s NAME`", build.Name, build.Name, app)
	}
	if !daemon.ValidEnvName(name) {
		return fmt.Errorf("%q is not an env name: letters, digits and _, like DATABASE_URL", name)
	}
	server, err := envServer(ctx, flags, app, stdin)
	if err != nil {
		return err
	}
	value, err := readEnvValue(stdin, stdout, name)
	if err != nil {
		return err
	}
	var saved daemon.EnvSaved
	if err := sshJSON(ctx, server, []string{"env", "set", app, name}, strings.NewReader(value), &saved); err != nil {
		return err
	}
	return startWithEnv(ctx, server, app, saved, stdout)
}

// readEnvValue asks for the value of name without showing it, or reads
// all of stdin, without the line break at its end.
func readEnvValue(stdin io.Reader, stdout io.Writer, name string) (string, error) {
	var value string
	if isTerminal(stdin) {
		fmt.Fprint(stdout, style.Warning.Render("?")+" Value of "+name+": ")
		typed, err := readSecret(stdin)
		fmt.Fprintln(stdout)
		if err != nil {
			return "", err
		}
		value = typed
	} else {
		body, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
		if err != nil {
			return "", err
		}
		value = strings.TrimSuffix(strings.TrimSuffix(string(body), "\n"), "\r")
	}
	if value == "" {
		return "", errors.New("no value, nothing changed")
	}
	return value, nil
}

// startWithEnv says what a change of the env did and starts the app with
// it. Before its first deploy the env waits for that deploy.
func startWithEnv(ctx context.Context, server, app string, saved daemon.EnvSaved, stdout io.Writer) error {
	rememberApp(server, app)
	if saved.None() {
		fmt.Fprintln(stdout, style.Muted.Render("Nothing changed."))
		return nil
	}
	if !saved.Live {
		done(stdout, "Saved on %s. It %s", server, saved.Change)
		hint(stdout, app+" starts with it on its first deploy:", build.Name+" deploy "+server)
		return nil
	}
	done(stdout, "Saved. It %s", saved.Change)
	result, err := runRelease(ctx, server, []string{"restart", app}, nil, stdout, "starting "+app+" with the new env")
	if err != nil {
		return err
	}
	done(stdout, "%s runs v%d with it", app, result.Release)
	return nil
}

// editUntilValid opens text in $EDITOR, in a folder only you can read,
// and opens it again while what you saved has a mistake. The folder goes
// afterwards, with any backup the editor left in it.
func editUntilValid(ctx context.Context, app string, text []byte, stdin io.Reader, stdout io.Writer) ([]byte, error) {
	dir, err := os.MkdirTemp("", build.Name+"-env-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, app+".env")
	if err := os.WriteFile(file, text, 0o600); err != nil {
		return nil, err
	}
	var mistake []byte
	for {
		if err := runEditor(ctx, file, stdin, stdout); err != nil {
			return nil, err
		}
		saved, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		_, parseErr := dotenv.Parse(string(saved))
		if parseErr == nil {
			return saved, nil
		}
		if bytes.Equal(saved, mistake) {
			return nil, fmt.Errorf("nothing changed, the file still has a mistake: %w", parseErr)
		}
		mistake = saved
		printError(stdout, fmt.Errorf("%w. Opening it again: fix it, or quit without saving to keep the env as it was", parseErr))
	}
}

// runEditor opens file in $EDITOR, or vi.
func runEditor(ctx context.Context, file string, stdin io.Reader, stdout io.Writer) error {
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) == 0 {
		editor = []string{"vi"}
	}
	cmd := exec.CommandContext(ctx, editor[0], append(editor[1:], file)...)
	// The editor draws on the terminal itself, not through the color writer.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, raw(stdout), os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.New("the editor did not end well, so nothing changed: " + err.Error())
	}
	return nil
}
