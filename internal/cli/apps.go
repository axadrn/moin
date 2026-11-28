package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/human"
	"github.com/axadrn/moin/internal/style"
)

// apps is `moin apps`: every app on every server, the table of `moin`.
func apps(ctx context.Context, args []string, stdout io.Writer) error {
	return overview(ctx, "apps", args, stdout)
}

// rollback is `moin rollback [app]`: an older release runs again, with
// the env and domains as they are now. On a terminal you pick it; --to v3
// names it; without either it is the release before.
func rollback(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("rollback", "rollback [app] [--to v3] [-s server] [--json]").withServer().withJSON()
	to := flags.String("to", "", "the release to go back to, like v3; default the one before the live one")
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	// With --json, stdout is for the JSON alone: the picker and the log go
	// to stderr.
	log := stdout
	if flags.json() {
		log = terminal(os.Stderr)
	}
	return onApp(ctx, flags, app, func(server string) error {
		number, err := rollbackTarget(ctx, server, app, *to, stdin, log)
		if err != nil {
			return err
		}
		result, err := rollBack(ctx, server, app, number, log)
		if err != nil {
			return err
		}
		if flags.json() {
			return printJSON(stdout, result)
		}
		done(stdout, "%s runs v%d again, as v%d", app, result.From, result.Release)
		return nil
	})
}

// rollbackTarget is the release to go back to: --to, else the one you
// pick on a terminal, else 0, the one before, which the server finds.
func rollbackTarget(ctx context.Context, server, app, to string, stdin io.Reader, stdout io.Writer) (int, error) {
	if to != "" {
		return releaseNumber(to)
	}
	if !isTerminal(stdin) {
		return 0, nil
	}
	releases, err := appReleases(ctx, server, app, os.Stderr)
	if err != nil {
		return 0, err
	}
	return pickRelease(stdin, stdout, app, releases, time.Now())
}

// appReleases are the releases of app that can run again, newest first.
func appReleases(ctx context.Context, server, app string, stderr io.Writer) ([]daemon.Release, error) {
	var releases []daemon.Release
	err := sshAnswer(ctx, server, []string{"releases", app}, nil, stderr, &releases)
	return releases, err
}

// rollBack runs an older release of app again: number, or the one before
// the live one when it is 0.
func rollBack(ctx context.Context, server, app string, number int, log io.Writer) (daemon.Result, error) {
	command := []string{"rollback", app}
	if number > 0 {
		command = append(command, "--to", strconv.Itoa(number))
	}
	return runRelease(ctx, server, command, nil, log, "rolling back "+app)
}

// pickRelease shows the releases and asks which one. Enter takes the one
// before the live one.
func pickRelease(stdin io.Reader, stdout io.Writer, app string, releases []daemon.Release, now time.Time) (int, error) {
	suggested := 0
	rows := [][]string{}
	for _, release := range releases {
		note := ""
		switch {
		case release.Live:
			note = badge(stdout, style.Badge(style.StatusLive, "live"), "live")
		case release.Before:
			note = style.Muted.Render("the one before")
			suggested = release.Number
		}
		rows = append(rows, []string{"v" + strconv.Itoa(release.Number), human.Ago(release.At, now), note})
	}
	if suggested == 0 {
		return 0, fmt.Errorf("%s has no older release to go back to", app)
	}
	table(stdout, []string{"DEPLOY", "WHEN", "STATE"}, rows)
	fmt.Fprint(stdout, style.Warning.Render("?")+" Go back to which release? "+style.Muted.Render("[v"+strconv.Itoa(suggested)+"]")+" ")
	answer, err := readLine(stdin)
	if err != nil {
		return 0, err
	}
	if answer == "" {
		return suggested, nil
	}
	return releaseNumber(answer)
}

// releaseNumber reads a release like v3 or 3.
func releaseNumber(text string) (int, error) {
	number, err := strconv.Atoi(strings.TrimPrefix(text, "v"))
	if err != nil || number < 1 {
		return 0, fmt.Errorf("%q is not a release: name one like v3", text)
	}
	return number, nil
}

// restart is `moin restart|start|stop [app]`. Restart and start run the
// live release again, with the env and domains as they are now; stop
// stops it until the next start.
func restart(ctx context.Context, command string, args []string, stdout io.Writer) error {
	flags := newFlags(command, command+" [app] [-s server]").withServer()
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		if command == "stop" {
			if err := sshRun(ctx, server, hereLine("stop", app), nil, io.Discard, os.Stderr); err != nil {
				return err
			}
			done(stdout, "%s is stopped. It stays stopped after a reboot, until `%s start %s`", app, build.Name, app)
			return nil
		}
		result, err := restartApp(ctx, server, app, stdout)
		if err != nil {
			return err
		}
		done(stdout, "%s runs v%d", app, result.Release)
		return nil
	})
}

// restartApp runs the live release of app again, with its env and domains
// as they are now. The steps go to log.
func restartApp(ctx context.Context, server, app string, log io.Writer) (daemon.Result, error) {
	return runRelease(ctx, server, []string{"restart", app}, nil, log, "starting "+app)
}

// logs is `moin logs [app] [-f]`: the last lines of the app's containers,
// and with -f what they write from then on.
func logs(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("logs", "logs [app] [-f] [--tail 200] [-s server] [--json]").withServer().withJSON()
	tail := flags.Int("tail", 200, "how many lines from the end")
	follow := new(bool)
	flags.BoolVar(follow, "f", false, "keep printing new lines until Ctrl-C")
	flags.BoolVar(follow, "follow", false, "keep printing new lines until Ctrl-C")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.json() && *follow {
		return errors.New("--json prints the lines there are, so it takes no -f")
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	return onApp(ctx, flags, app, func(server string) error {
		if flags.json() {
			var out strings.Builder
			if err := appLogs(ctx, server, app, *tail, false, &out, os.Stderr); err != nil {
				return err
			}
			return printJSON(stdout, logLines(out.String()))
		}
		// On a terminal each line shows its time short and its
		// container dimmed; in a pipe the lines stay as they come.
		out := raw(stdout)
		if isTTY(stdout) {
			pretty := &lineWriter{w: stdout, format: style.AppLog}
			defer pretty.flush()
			out = pretty
		}
		err := appLogs(ctx, server, app, *tail, *follow, out, os.Stderr)
		if ctx.Err() != nil {
			// Ctrl-C ends -f: that is how it is meant to stop.
			return nil
		}
		return err
	})
}

// appLogs writes the last tail lines of the app's containers to stdout,
// and with follow what they write from then on, until ctx ends.
func appLogs(ctx context.Context, server, app string, tail int, follow bool, stdout, stderr io.Writer) error {
	command := []string{"logs", app, "--tail", strconv.Itoa(tail)}
	if follow {
		command = append(command, "--follow")
	}
	return sshRun(ctx, server, hereLine(command...), nil, stdout, stderr)
}

// logLine is one line of `moin logs --json`.
type logLine struct {
	Time      string `json:"time"`
	Container string `json:"container"`
	Line      string `json:"line"`
}

// logLines reads what docker compose logs --timestamps prints, a line like
// "web-1  | 2026-11-27T16:39:08.672243096Z hello". A line without the
// container or the time keeps them empty.
func logLines(text string) []logLine {
	lines := []logLine{}
	for raw := range strings.Lines(text) {
		container, stamp, rest := style.SplitAppLog(raw)
		lines = append(lines, logLine{Time: stamp, Container: container, Line: rest})
	}
	return lines
}

// lineWriter writes each whole line through format, for logs on a
// terminal. flush writes what is left of a last line.
type lineWriter struct {
	w      io.Writer
	format func(string) string
	rest   []byte
}

func (l *lineWriter) Write(p []byte) (int, error) {
	l.rest = append(l.rest, p...)
	for {
		end := bytes.IndexByte(l.rest, '\n')
		if end < 0 {
			return len(p), nil
		}
		if _, err := fmt.Fprintln(l.w, l.format(string(l.rest[:end]))); err != nil {
			return 0, err
		}
		l.rest = l.rest[end+1:]
	}
}

func (l *lineWriter) flush() {
	if len(l.rest) > 0 {
		fmt.Fprintln(l.w, l.format(string(l.rest)))
		l.rest = nil
	}
}

// execArgs splits `exec [app] [flags] -- <command…>` at the first "--":
// what comes before is moin's, what comes after is the command's own,
// flags and all.
func execArgs(args []string) (own, command []string) {
	cut := slices.Index(args, "--")
	if cut < 0 {
		return args, nil
	}
	return args[:cut], args[cut+1:]
}

// execCommand is `moin exec [app] -- <command>`: it runs a command in the
// app's container, on a terminal like a shell, and exits with its code.
func execCommand(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	own, command := execArgs(args)
	flags := newFlags("exec", "exec [app] [--service web] [-s server] -- <command> [args]").withServer()
	service := flags.String("service", "", "the compose service to run it in; default the app's main service")
	if err := flags.parse(own); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	if len(command) == 0 {
		return fmt.Errorf("name the command after --, like `%s exec %s -- ls`", build.Name, app)
	}
	tty := isTerminal(stdin)
	line := []string{"exec", app}
	if *service != "" {
		line = append(line, "--service", *service)
	}
	if tty {
		line = append(line, "--tty")
	}
	line = append(append(line, "--"), command...)
	return onApp(ctx, flags, app, func(server string) error {
		cmd := sshCommand(ctx, server, asRoot(hereLine(line...)), tty)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, raw(stdout), os.Stderr
		// Every exit code is the command's own, also 97, 98 or 255: read
		// as moin's, a command could run twice.
		err := cmd.Run()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exitCode(exit.ExitCode())
		}
		return err
	})
}

// appState is how an app runs, in words and with its color on a terminal.
func appState(w io.Writer, running bool) string {
	if running {
		return badge(w, style.Badge(style.StatusLive, "running"), "running")
	}
	return badge(w, style.Badge(style.StatusIdle, "stopped"), "stopped")
}
