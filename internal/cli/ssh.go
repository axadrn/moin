package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
	"github.com/axadrn/moin/protocol"
)

// The CLI reaches a server the way `ssh <server>` does: the server is a
// name from your ssh config, or user@host. moin runs on the server as
// `moin here …` and does the work there. No hub in between.

const remoteBinary = "/usr/local/bin/" + build.Name

// sshDeploy is `moin deploy <server>`: it sends the committed source of
// this folder to the server, which builds and starts it.
func sshDeploy(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("deploy", "deploy <server> [--app name] [--yes]").withApp().withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return flags.wrong()
	}
	server := flags.Arg(0)
	app, err := folderApp(*flags.appFlag)
	if err != nil {
		return err
	}
	commit, err := gitOutput(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		return errors.New("deploy sends the committed source: run it in a git repository with a commit")
	}
	if dirty, _ := gitOutput(ctx, "status", "--porcelain"); dirty != "" {
		fmt.Fprintln(stdout, style.Muted.Render("Changes that are not committed stay here: moin sends "+commit+"."))
	}
	fmt.Fprintf(stdout, "%s · %s → %s\n", app, commit, server)

	installed, err := sshOutput(ctx, server, "command -v "+build.Name+" docker || true")
	if err != nil {
		return err
	}
	if !strings.Contains(installed, remoteBinary) || !strings.Contains(installed, "docker") {
		if err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Set up %s on %s and deploy %s?", build.Name, server, app)); err != nil {
			return err
		}
	}
	if !strings.Contains(installed, "docker") {
		fmt.Fprintln(stdout, style.Muted.Render("Installing Docker…"))
		if err := sshRun(ctx, server, "curl -fsSL https://get.docker.com | sh", nil, io.Discard); err != nil {
			return fmt.Errorf("install Docker on %s: %w", server, err)
		}
	}
	if err := sendBinary(ctx, server); err != nil {
		return err
	}

	archive := exec.CommandContext(ctx, "git", "archive", "--format=tar.gz", "HEAD")
	source, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	if err := archive.Start(); err != nil {
		return err
	}
	runErr := sshRun(ctx, server, build.Name+" here deploy "+app, source, stdout)
	if err := archive.Wait(); err != nil && runErr == nil {
		return fmt.Errorf("git archive: %w", err)
	}
	if runErr != nil {
		return errors.New("the deploy failed: the lines above say why")
	}
	return nil
}

// sshLogs is `moin logs <server> [app]`.
func sshLogs(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("logs", "logs <server> [--app name] [--tail 200]").withApp()
	tail := flags.Int("tail", 200, "how many lines")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return flags.wrong()
	}
	app, err := folderApp(*flags.appFlag)
	if err != nil {
		return err
	}
	return sshRun(ctx, flags.Arg(0), build.Name+" here logs "+app+" "+strconv.Itoa(*tail), nil, stdout)
}

// here is `moin here …`, what the CLI runs on a server over SSH.
func here(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) < 2 || !protocol.ValidAppName(args[1]) {
		return errors.New("moin here runs on a server, for the CLI on your laptop")
	}
	switch args[0] {
	case "deploy":
		return daemon.DeployHere(ctx, daemon.StateDir()+"/apps", args[1], stdin, stdout)
	case "logs":
		tail := 200
		if len(args) > 2 {
			tail, _ = strconv.Atoi(args[2])
		}
		return daemon.LogsHere(ctx, daemon.StateDir()+"/apps", args[1], tail, stdout)
	}
	return fmt.Errorf("unknown: moin here %s", args[0])
}

// sendBinary puts this very binary on the server when the server has
// another one. A release takes the signed binary of its version instead.
func sendBinary(ctx context.Context, server string) error {
	arch, err := sshOutput(ctx, server, "uname -sm")
	if err != nil {
		return err
	}
	want := map[string]string{"x86_64": "amd64", "aarch64": "arm64"}[strings.Fields(arch + " _")[1]]
	if runtime.GOOS != "linux" || want != runtime.GOARCH {
		return fmt.Errorf("%s is %s: this build of moin is %s/%s and can't run there yet", server, strings.TrimSpace(arch), runtime.GOOS, runtime.GOARCH)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	body, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	there, _ := sshOutput(ctx, server, "sha256sum "+remoteBinary+" 2>/dev/null || true")
	if strings.HasPrefix(there, hex.EncodeToString(sum[:])) {
		return nil
	}
	return sshRun(ctx, server, "cat > "+remoteBinary+".new && chmod 755 "+remoteBinary+".new && mv "+remoteBinary+".new "+remoteBinary, bytes.NewReader(body), io.Discard)
}

// folderApp is the app's name: --app, or this folder's name.
func folderApp(flag string) (string, error) {
	name := flag
	if name == "" {
		dir, err := os.Getwd()
		if err != nil {
			return "", err
		}
		name = strings.ToLower(filepath.Base(dir))
	}
	if !protocol.ValidAppName(name) {
		return "", fmt.Errorf("%q is not a valid app name: pass --app with lowercase letters, digits and dashes", name)
	}
	return name, nil
}

// sshRun runs command on server through the ssh of this computer, with
// its config, keys and agent.
func sshRun(ctx context.Context, server, command string, stdin io.Reader, stdout io.Writer) error {
	cmd := exec.CommandContext(ctx, "ssh", server, command)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, os.Stderr
	return cmd.Run()
}

func sshOutput(ctx context.Context, server, command string) (string, error) {
	var out bytes.Buffer
	if err := sshRun(ctx, server, command, nil, &out); err != nil {
		return "", fmt.Errorf("ssh %s: %w", server, err)
	}
	return out.String(), nil
}

func gitOutput(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	return strings.TrimSpace(string(out)), err
}
