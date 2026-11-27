package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/style"
)

// The CLI reaches a server the way `ssh <server>` does: the server is a
// name from your ssh config, or user@host, with your keys and agent. moin
// runs there as `moin here …`, as root, and does the work. Its data comes
// back on stdout, what it says to people on stderr.

// remoteBinary is where moin lives on a server.
const remoteBinary = "/usr/local/bin/" + build.Name

// noSudo is the exit code of a command for root that the SSH user may not
// run with sudo.
const noSudo = 97

// sshTimeout is how many seconds ssh waits for a server to answer.
const sshTimeout = "10"

// hereLine is the shell line that runs `moin here args…` on a server.
func hereLine(args ...string) string {
	return shellJoin(append([]string{remoteBinary, "here"}, args...))
}

// asRoot runs line as root: as it is for root, with sudo -n for anyone
// else, and exits with noSudo when sudo would ask for a password.
func asRoot(line string) string {
	return `if [ "$(id -u)" = 0 ]; then ` + line + `; else sudo -n true 2>/dev/null || exit ` + strconv.Itoa(noSudo) + `; sudo -n ` + line + `; fi`
}

// shellJoin quotes each word for the remote shell, so nothing in it is
// read twice: a command for exec reaches the container word for word.
func shellJoin(words []string) string {
	quoted := make([]string, len(words))
	for i, word := range words {
		quoted[i] = "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

// sshCommand is ssh to server running line. tty asks for a terminal on the
// server, for exec on a terminal.
func sshCommand(ctx context.Context, server, line string, tty bool) *exec.Cmd {
	args := []string{"-o", "ConnectTimeout=" + sshTimeout}
	if tty {
		args = append(args, "-t")
	}
	// After --, a server name never reads as an option of ssh.
	args = append(args, "--", server, line)
	return exec.CommandContext(ctx, "ssh", args...)
}

// sshRun runs line on server as root with stdin, stdout and stderr passed
// through.
func sshRun(ctx context.Context, server, line string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := sshCommand(ctx, server, asRoot(line), false)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return sshError(server, cmd.Run())
}

// sshOutput runs line on server as root and returns what it printed.
func sshOutput(ctx context.Context, server, line string, stdin io.Reader) ([]byte, error) {
	var out bytes.Buffer
	err := sshRun(ctx, server, line, stdin, &out, os.Stderr)
	return out.Bytes(), err
}

// sshLog runs line on server as root and shows what it says on stderr as a
// deploy log: steps with →, tool output dimmed, a spinner on a terminal.
// It returns what the server printed on stdout. When moin there fails,
// its own ✗ line becomes the error.
func sshLog(ctx context.Context, server, line string, stdin io.Reader, stdout io.Writer, first string) ([]byte, error) {
	reader, writer := io.Pipe()
	shown := make(chan struct{})
	progress := newProgress(stdout, first)
	failure := ""
	go func() {
		defer close(shown)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			if message, ok := strings.CutPrefix(scanner.Text(), style.SymbolFailed+" "); ok {
				failure = message
				continue
			}
			progress.line(scanner.Text())
		}
	}()
	var out bytes.Buffer
	err := sshRun(ctx, server, line, stdin, &out, writer)
	writer.Close()
	<-shown
	progress.end()
	var code exitCode
	if errors.As(err, &code) && failure != "" {
		return nil, errors.New(failure)
	}
	return out.Bytes(), err
}

// sshError says what went wrong in words: ssh exits with 255 when it
// cannot reach the server, asRoot with noSudo. Any other code is moin's
// on the server, which said why on stderr already.
func sshError(server string, err error) error {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return err
	}
	switch exit.ExitCode() {
	case 255:
		return fmt.Errorf("cannot reach %s over SSH. Check that `ssh %s` works", server, server)
	case noSudo:
		return fmt.Errorf("%s runs as root on %s, and your user there may not use sudo without a password. Log in as root, or allow sudo for it", build.Name, server)
	case noAppCode:
		return errNoApp
	}
	return exitCode(exit.ExitCode())
}

// exitCode is an error that is already on the screen and ends moin with
// its code, like the exit code of a command run with exec.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// sshJSON runs `moin here args…` on server as root and reads the JSON it
// answers into value.
func sshJSON(ctx context.Context, server string, args []string, stdin io.Reader, value any) error {
	return sshAnswer(ctx, server, args, stdin, os.Stderr, value)
}

// quietJSON is sshJSON for many servers at once: without a password
// prompt and without what ssh says on stderr. Many prompts at once would
// fight over the terminal, and the noise would mix into the table.
func quietJSON(ctx context.Context, server string, args []string, value any) error {
	cmd := sshCommand(ctx, server, asRoot(hereLine(args...)), false)
	// BatchMode: a server that would ask for a password fails instead.
	cmd.Args = slices.Insert(cmd.Args, 1, "-o", "BatchMode=yes")
	out, err := cmd.Output()
	if err != nil {
		return sshError(server, err)
	}
	return readAnswer(server, out, value)
}

// sshAnswer is sshJSON with what ssh and moin say going to stderr, so the
// terminal UI can keep it off its screen.
func sshAnswer(ctx context.Context, server string, args []string, stdin io.Reader, stderr io.Writer, value any) error {
	var out bytes.Buffer
	if err := sshRun(ctx, server, hereLine(args...), stdin, &out, stderr); err != nil {
		return err
	}
	return readAnswer(server, out.Bytes(), value)
}

// readAnswer reads what a server answered. A server with another version
// of moin may answer something else.
func readAnswer(server string, out []byte, value any) error {
	if err := json.Unmarshal(out, value); err != nil {
		return fmt.Errorf("%s answered something %s cannot read. Put this version there with `%s update %s`", server, build.Name, build.Name, server)
	}
	return nil
}
