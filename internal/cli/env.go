package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	after, err := editText(ctx, app, before, stdin, stdout)
	if err != nil {
		return err
	}
	if bytes.Equal(before, after) {
		fmt.Fprintln(stdout, style.Muted.Render("Nothing changed."))
		return nil
	}
	if _, err := dotenv.Parse(string(after)); err != nil {
		return fmt.Errorf("nothing changed, the file has a mistake: %w", err)
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

// editText opens text in $EDITOR, or vi, in a file only you can read, and
// returns what was saved. The file is gone afterwards.
func editText(ctx context.Context, app string, text []byte, stdin io.Reader, stdout io.Writer) ([]byte, error) {
	file, err := os.CreateTemp("", build.Name+"-"+app+"-*.env")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(text)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	editor := strings.Fields(os.Getenv("EDITOR"))
	if len(editor) == 0 {
		editor = []string{"vi"}
	}
	cmd := exec.CommandContext(ctx, editor[0], append(editor[1:], file.Name())...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, errors.New("the editor did not end well, so nothing changed: " + err.Error())
	}
	return os.ReadFile(file.Name())
}
