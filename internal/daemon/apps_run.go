package daemon

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// composeArgs are docker's arguments for compose on a compose file of the
// app: its project, the file, and the env files that fill each ${NAME} in
// it (envArgs). Every compose command on a release goes through here, so a
// value is filled in the same way for up, stop, ps, exec and the rest.
func (a *apps) composeArgs(app, composeFile string, args ...string) []string {
	base := []string{"compose", "-p", a.projectOf(app), "-f", composeFile}
	base = append(base, a.envArgs(app, filepath.Dir(composeFile))...)
	return append(base, args...)
}

// compose runs docker compose on a release and streams its output as log
// lines.
func (a *apps) compose(ctx context.Context, app, composeFile string, logf func(string, ...any), args ...string) error {
	return a.run(ctx, logf, nil, a.composeArgs(app, composeFile, append([]string{"--progress", "plain"}, args...)...)...)
}

// run runs docker with extra environment and streams its output as log lines.
func (a *apps) run(ctx context.Context, logf func(string, ...any), environment []string, args ...string) error {
	command := exec.CommandContext(ctx, "docker", args...)
	if environment != nil {
		command.Env = append(os.Environ(), environment...)
	}
	reader, writer := io.Pipe()
	command.Stdout, command.Stderr = writer, writer
	lines := make(chan struct{})
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			logf("%s", scanner.Text())
		}
	}()
	err := command.Run()
	writer.Close()
	<-lines
	return err
}

// output runs a command and returns its stdout. A failure carries stderr.
func (a *apps) output(ctx context.Context, name string, args ...string) (string, error) {
	return a.outputEnv(ctx, nil, name, args...)
}

// outputEnv is output with extra environment variables. A secret goes
// there, not in args: args show in ps to every user of the server.
func (a *apps) outputEnv(ctx context.Context, env []string, name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, name, args...)
	if env != nil {
		command.Env = append(os.Environ(), env...)
	}
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return "", err
		}
		return "", errors.New(message)
	}
	return stdout.String(), nil
}

// stopServices stops services of an app, all without names, and returns
// what starts them again, with a context a stopping daemon does not cancel.
func (a *apps) stopServices(ctx context.Context, app string, services ...string) (start func() error, err error) {
	ctx = context.WithoutCancel(ctx)
	run := func(args ...string) error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		return a.compose(ctx, app, a.composeFile(app), func(string, ...any) {}, append(args, services...)...)
	}
	start = func() error { return run("start") }
	if err := run("stop"); err != nil {
		return nil, errors.Join(fmt.Errorf("stop the app: %w; nothing changed", err), start())
	}
	return start, nil
}
