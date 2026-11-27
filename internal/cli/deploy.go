package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

// deploy is `moin deploy [server]`: it sends the committed source of this
// folder to the server, which builds and starts it. The first deploy to a
// server sets moin up there; later ones go to where the app runs.
func deploy(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("deploy", "deploy [server] [--image <local image> | --catalog <name>] [--app <name>] [--yes]").withApp().withYes().withJSON()
	image := flags.String("image", "", "send this local image, like shop:dev, instead of building on the server")
	catalogApp := flags.String("catalog", "", "deploy an app of the catalog, like umami, without a repo")
	if err := flags.parse(args); err != nil {
		return err
	}
	if *image != "" && *catalogApp != "" {
		return errors.New("deploy takes --image or --catalog, not both")
	}
	app, err := deployedApp(flags, *catalogApp)
	if err != nil {
		return err
	}
	server, err := deployServer(ctx, flags, app)
	if err != nil {
		return err
	}
	// With --json, stdout is for the JSON alone: the log goes to stderr.
	log := stdout
	if flags.json() {
		log = terminal(os.Stderr)
	}
	from, err := deploySource(ctx, *image, *catalogApp, log)
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "%s · %s → %s\n", style.Bold.Render(app), from, server)
	if err := setUp(ctx, server, app, flags.yes(), stdin, log); err != nil {
		return err
	}
	if *image != "" {
		if err := sendImage(ctx, server, *image, log); err != nil {
			return err
		}
	}
	result, err := runDeploy(ctx, server, app, *image, *catalogApp, log)
	if err != nil {
		return err
	}
	if err := addServer(server); err != nil {
		return err
	}
	rememberApp(server, app)
	if flags.json() {
		return printJSON(stdout, result)
	}
	printLive(stdout, server, result)
	return nil
}

// deployedApp is the app's name: --app, the catalog app's name, or this
// folder's.
func deployedApp(flags *flagSet, catalogApp string) (string, error) {
	if catalogApp != "" && *flags.appFlag == "" {
		*flags.appFlag = catalogApp
	}
	return flags.app()
}

// deployServer is where the deploy goes: the server named, else the one
// that runs the app already.
func deployServer(ctx context.Context, flags *flagSet, app string) (string, error) {
	if flags.NArg() == 1 {
		return flags.Arg(0), nil
	}
	if flags.NArg() > 1 {
		return "", flags.wrong()
	}
	servers := knownServers(app)
	if len(servers) == 0 {
		found, err := findServers(ctx, app)
		if err != nil {
			return "", err
		}
		servers = found
	}
	switch len(servers) {
	case 0:
		return "", fmt.Errorf("where to? The first deploy of %s names the server: `%s deploy <server>`, a name from your ssh config or user@host", app, build.Name)
	case 1:
		return servers[0], nil
	}
	return "", fmt.Errorf("%s runs on %s. Name the one to deploy to: `%s deploy %s`", app, strings.Join(servers, " and "), build.Name, servers[0])
}

// deploySource says what the deploy sends, like "3f2a9c1" or "catalog
// umami". A deploy of this folder needs a commit: moin sends what is
// committed.
func deploySource(ctx context.Context, image, catalogApp string, stdout io.Writer) (string, error) {
	if catalogApp != "" {
		return "catalog " + catalogApp, nil
	}
	commit, err := gitOutput(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", errors.New("deploy sends the committed source of this folder: run it in a git repository with a commit, or deploy an app of the catalog with --catalog")
	}
	if dirty, _ := gitOutput(ctx, "status", "--porcelain"); dirty != "" {
		fmt.Fprintln(stdout, style.Muted.Render("Changes that are not committed stay here: moin sends "+commit+"."))
	}
	if image != "" {
		return commit + " with image " + image, nil
	}
	return commit, nil
}

// runDeploy runs the deploy on the server and shows its log as it comes.
func runDeploy(ctx context.Context, server, app, image, catalogApp string, stdout io.Writer) (daemon.Result, error) {
	args := []string{"deploy", app}
	if image != "" {
		args = append(args, "--image", image)
	}
	if catalogApp != "" {
		args = append(args, "--catalog", catalogApp)
	}
	var source io.Reader
	if catalogApp == "" {
		archive := exec.CommandContext(ctx, "git", "archive", "--format=tar.gz", "HEAD")
		pipe, err := archive.StdoutPipe()
		if err != nil {
			return daemon.Result{}, err
		}
		if err := archive.Start(); err != nil {
			return daemon.Result{}, err
		}
		defer archive.Wait()
		source = pipe
	}
	return runRelease(ctx, server, args, source, stdout, "sending the source")
}

// runRelease runs a command on the server that starts a release, like a
// deploy, a rollback or a restart, shows its log and returns what runs.
func runRelease(ctx context.Context, server string, args []string, stdin io.Reader, stdout io.Writer, first string) (daemon.Result, error) {
	out, err := sshLog(ctx, server, hereLine(args...), stdin, stdout, first)
	var code exitCode
	if errors.As(err, &code) {
		return daemon.Result{}, fmt.Errorf("it did not work on %s: the lines above say why", server)
	}
	if err != nil {
		return daemon.Result{}, err
	}
	var result daemon.Result
	return result, readAnswer(server, out, &result)
}

// printLive says the app is live, with its address and what to do next.
func printLive(stdout io.Writer, server string, result daemon.Result) {
	if len(result.Addresses) == 0 {
		done(stdout, "%s. %s runs v%d. It has no address yet: `%s domains add %s <domain>`", build.Name, result.App, result.Release, build.Name, server)
		return
	}
	done(stdout, "%s. %s is live: %s", build.Name, result.App, result.Addresses[0])
	for _, address := range result.Addresses[1:] {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("also "+address))
	}
	if len(result.Databases) > 0 {
		hint(stdout, strings.Join(result.Databases, " and ")+" found. See its backups:", build.Name+" backups "+server)
	}
}

// sendImage puts a local image on the server: docker save here, docker
// load there.
func sendImage(ctx context.Context, server, image string, stdout io.Writer) error {
	step(stdout, "sending image %s", image)
	save := exec.CommandContext(ctx, "docker", "save", image)
	save.Stderr = os.Stderr
	pipe, err := save.StdoutPipe()
	if err != nil {
		return err
	}
	if err := save.Start(); err != nil {
		return fmt.Errorf("docker save %s: %w", image, err)
	}
	loadErr := sshRun(ctx, server, shellJoin([]string{"docker", "load"}), pipe, io.Discard, os.Stderr)
	if err := save.Wait(); err != nil {
		return fmt.Errorf("docker save %s: %w. Is the image on this computer? See `docker images`", image, err)
	}
	return loadErr
}

// serverInfo is what a server has before a deploy.
type serverInfo struct {
	arch    string // like amd64
	docker  bool
	version string // moin's version, "" without moin
}

// probe looks at the server with one SSH call, as the SSH user.
func probe(ctx context.Context, server string) (serverInfo, error) {
	line := "uname -m; command -v docker >/dev/null && echo docker; " + remoteBinary + " version 2>/dev/null | head -n 1; true"
	out, err := sshCommand(ctx, server, line, false).Output()
	if err != nil {
		return serverInfo{}, sshError(server, err)
	}
	info := serverInfo{}
	for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		switch {
		case i == 0:
			info.arch = goArch(line)
		case line == "docker":
			info.docker = true
		case strings.HasPrefix(line, build.Name+" "):
			info.version = strings.TrimPrefix(line, build.Name+" ")
		}
	}
	return info, nil
}

// goArch is Go's name for what uname -m says, "" for one moin has no
// build for.
func goArch(machine string) string {
	switch strings.TrimSpace(machine) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	}
	return ""
}

// setUp installs Docker and moin on a server that lacks them, after a yes.
// A server with another version of moin gets a hint, never an update.
func setUp(ctx context.Context, server, app string, yes bool, stdin io.Reader, stdout io.Writer) error {
	info, err := probe(ctx, server)
	if err != nil {
		return err
	}
	if info.docker && info.version != "" {
		warnOtherVersion(stdout, server, info.version)
		return nil
	}
	if info.arch == "" {
		return fmt.Errorf("%s is not a 64-bit Intel or ARM server, which moin needs", server)
	}
	question := fmt.Sprintf("Set up %s on %s and deploy %s?", build.Name, server, app)
	if err := confirm(stdin, stdout, yes, question); err != nil {
		return err
	}
	if !info.docker {
		if _, err := sshLog(ctx, server, shellJoin([]string{"sh", "-c", "curl -fsSL https://get.docker.com | sh"}), nil, stdout, "installing Docker"); err != nil {
			return fmt.Errorf("install Docker on %s: %w", server, err)
		}
	}
	if info.version != "" {
		return nil
	}
	step(stdout, "installing %s %s", build.Name, build.Version)
	if err := installMoin(ctx, server, info.arch); err != nil {
		return err
	}
	step(stdout, "starting the daemon: backups, restore checks, reports")
	return sshRun(ctx, server, hereLine("setup", serverName(server)), nil, io.Discard, os.Stderr)
}

// serverName is the name the server gets on its own, for its backups and
// its watchers: prod for prod, the host for user@host.
func serverName(server string) string {
	_, host, found := strings.Cut(server, "@")
	if found {
		return host
	}
	return server
}

// warnOtherVersion says when the server runs another moin than this CLI.
func warnOtherVersion(stdout io.Writer, server, version string) {
	if version == build.Version || !build.IsRelease(version) || !build.IsRelease(build.Version) {
		return
	}
	fmt.Fprintln(stdout, style.Warning.Render(fmt.Sprintf("%s runs %s %s, this CLI is %s.", server, build.Name, version, build.Version)))
	hint(stdout, "Put this version there with", build.Name+" update "+server)
}

// installMoin puts this CLI's version of moin on a server: for a release
// the signed binary of the server's platform, else this very binary.
func installMoin(ctx context.Context, server, arch string) error {
	body, err := moinFor(ctx, arch)
	if err != nil {
		return err
	}
	return sendBinary(ctx, server, body)
}

// moinFor is the moin binary of this version for a linux server.
func moinFor(ctx context.Context, arch string) ([]byte, error) {
	if build.IsRelease(build.Version) {
		return daemon.FetchRelease(ctx, build.Version, "linux", arch)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != arch {
		return nil, fmt.Errorf("this build of moin is %s/%s and cannot run on a linux/%s server. Use a release, or build moin for linux/%s", runtime.GOOS, runtime.GOARCH, arch, arch)
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(self)
}

// sendBinary writes body to the server as its moin, checks that it runs,
// and restarts the daemon when one runs.
func sendBinary(ctx context.Context, server string, body []byte) error {
	script := "set -e; mkdir -p /usr/local/bin; cat > " + remoteBinary + ".new; chmod 755 " + remoteBinary + ".new; " +
		remoteBinary + ".new version >/dev/null; mv " + remoteBinary + ".new " + remoteBinary + "; " +
		"systemctl try-restart " + build.Name + "-daemon 2>/dev/null || true"
	err := sshRun(ctx, server, shellJoin([]string{"sh", "-c", script}), bytes.NewReader(body), io.Discard, os.Stderr)
	if err != nil {
		return fmt.Errorf("put %s on %s: %w", build.Name, server, err)
	}
	return nil
}

// gitOutput runs git in this folder and returns what it printed.
func gitOutput(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	return strings.TrimSpace(string(out)), err
}
