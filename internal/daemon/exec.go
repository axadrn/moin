package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/protocol"
)

// execArgs is docker's argv for a run: compose exec in the app's live
// release, without a TTY. The command follows the service as words of its
// own, so no shell ever reads it.
func (a *apps) execArgs(app, composeJSON, service string, argv []string) []string {
	return append(a.composeArgs(app, composeJSON, "exec", "-T", service), argv...)
}

// execTimeout is the run's timeout in seconds: the default when none is
// asked, and never more than the most.
func execTimeout(seconds int) time.Duration {
	switch {
	case seconds <= 0:
		seconds = protocol.ExecTimeout
	case seconds > protocol.ExecMaxTimeout:
		seconds = protocol.ExecMaxTimeout
	}
	return time.Duration(seconds) * time.Second
}

// runExec runs one command in a container of an app and hands what it
// writes to output as it comes, up to protocol.ExecMaxOutput. The process
// docker started inside the container is not ours to kill: after the
// timeout we stop waiting, and a command that ignores it may go on there.
func (a *apps) runExec(ctx context.Context, start protocol.ExecStart, output func(string)) (protocol.ExecDone, error) {
	if !protocol.ValidAppName(start.App) || !validService(start.Service) {
		return protocol.ExecDone{}, errors.New("invalid app or service name")
	}
	if len(start.Argv) == 0 || start.Argv[0] == "" {
		return protocol.ExecDone{}, errors.New("no command")
	}
	current := a.current(start.App)
	if current == 0 {
		return protocol.ExecDone{}, fmt.Errorf("%s has no live deploy: deploy it first", start.App)
	}
	composeJSON := filepath.Join(a.releaseDir(start.App, current), "compose.json")
	ctx, cancel := context.WithTimeout(ctx, execTimeout(start.Timeout))
	defer cancel()
	command := exec.CommandContext(ctx, "docker", a.execArgs(start.App, composeJSON, start.Service, start.Argv)...)
	command.WaitDelay = 5 * time.Second
	reader, writer := io.Pipe()
	command.Stdout, command.Stderr = writer, writer
	var done protocol.ExecDone
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		done.Truncated = copyCapped(reader, protocol.ExecMaxOutput, output)
	}()
	err := command.Run()
	writer.Close()
	<-copied
	done.Command = a.maskedCommand(start.App, start.Argv)
	var exitErr *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		done.TimedOut = true
		done.ExitCode = 124
	case err == nil:
	case errors.As(err, &exitErr):
		done.ExitCode = exitErr.ExitCode()
	default:
		return protocol.ExecDone{}, err
	}
	return done, nil
}

// maskedCommand is argv as one line for the log, with the app's env
// values masked, such as a password typed into the command. Short values would mask
// ordinary words, so only values of six characters or more count. When the
// env file cannot be read, only the program is left.
func (a *apps) maskedCommand(app string, argv []string) string {
	env, err := a.env(app)
	if err != nil {
		return argv[0] + " …"
	}
	var values []string
	for _, value := range env {
		if len(value) >= 6 {
			values = append(values, value)
		}
	}
	return maskValues(protocol.JoinCommand(argv), values)
}

// maskValues replaces every value in text with •••, the longest first, so
// a value inside another is masked as part of the longer one.
func maskValues(text string, values []string) string {
	slices.SortFunc(values, func(a, b string) int { return len(b) - len(a) })
	for _, value := range values {
		text = strings.ReplaceAll(text, value, "•••")
	}
	return text
}

// copyCapped hands what reader gives to output in pieces, at most limit
// bytes in all, and reads the rest away. It reports whether it cut.
func copyCapped(reader io.Reader, limit int, output func(string)) (truncated bool) {
	buffer := make([]byte, 16*1024)
	sent := 0
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			piece := buffer[:n]
			if sent+n > limit {
				piece = piece[:max(limit-sent, 0)]
				truncated = true
			}
			if len(piece) > 0 {
				output(string(piece))
				sent += len(piece)
			}
		}
		if err != nil {
			return truncated
		}
	}
}
