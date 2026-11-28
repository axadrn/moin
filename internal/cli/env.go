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
	"github.com/axadrn/moin/internal/dotenv"
	"github.com/axadrn/moin/internal/style"
)

// env is `moin env [app]`, the names of the app's env vars, and
// `moin env edit [app]`. A value is never printed: it only shows in
// your editor.
func env(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "edit" {
		return envEdit(ctx, args[1:], stdin, stdout)
	}
	flags := newFlags("env", "env [app] [-s server] [--json], or env edit [app]").withServer().withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
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
	})
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
		return errors.New("env edit needs a terminal: it opens your $EDITOR")
	}
	return onApp(ctx, flags, app, func(server string) error {
		return editEnv(ctx, server, app, stdin, stdout)
	})
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
	var change dotenv.Change
	if err := sshJSON(ctx, server, []string{"env", "set", app}, bytes.NewReader(after), &change); err != nil {
		return err
	}
	if change.None() {
		fmt.Fprintln(stdout, style.Muted.Render("Nothing changed."))
		return nil
	}
	done(stdout, "Saved. It %s", change)
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
