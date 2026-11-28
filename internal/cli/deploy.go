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
	flags := newFlags("deploy", "deploy [server] [--image <image> | --catalog <name>] [--app <name>] [--no-copy] [-y] [--json]").withServer().withYes().withJSON()
	noCopy := flags.Bool("no-copy", false, "deploy without the copy of the databases just before it, when that copy fails")
	image := flags.String("image", "", "run this image instead of building: the server pulls it, like ghcr.io/you/my-app:1.4, or one only this computer has, like my-app:dev, is sent")
	catalogApp := flags.String("catalog", "", "deploy an app of the catalog, like umami, without a repo")
	appName := flags.String("app", "", "the app's name; default this folder's name, or the catalog app's")
	if err := flags.parse(args); err != nil {
		return err
	}
	if *image != "" && *catalogApp != "" {
		return errors.New("deploy takes --image or --catalog, not both")
	}
	app, err := deployedApp(*appName, *catalogApp)
	if err != nil {
		return err
	}
	// With --json, stdout is for the JSON alone: the log goes to stderr.
	log := stdout
	if flags.json() {
		log = terminal(os.Stderr)
	}
	// What to send first: without a commit there is nothing to deploy,
	// wherever it goes.
	from, err := deploySource(ctx, *image, *catalogApp, log)
	if err != nil {
		return err
	}
	// A deploy of this folder sends the commit from here, pushed or not.
	var pushed *bool
	if *image == "" && *catalogApp == "" {
		remote, ok := onRemote(ctx)
		pushed = &ok
		if !ok && !flags.json() {
			fmt.Fprintln(log, style.Warning.Render("!")+" "+from+" is not on "+remote+" yet. Deploying it anyway.")
		}
	}
	server, err := deployServer(ctx, flags, app, stdin)
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "%s · %s → %s\n", style.Bold.Render(app), from, server)
	if err := setUp(ctx, server, app, flags.yes(), stdin, log); err != nil {
		return err
	}
	// In the list from now on, so `moin` shows the server even when this
	// first deploy fails.
	if err := addServer(server); err != nil {
		return err
	}
	if *image != "" {
		if err := bringImage(ctx, server, *image, log); err != nil {
			return err
		}
	}
	result, err := runDeploy(ctx, server, app, *image, *catalogApp, *noCopy, log)
	if err != nil {
		return err
	}
	rememberApp(server, app)
	if flags.json() {
		return printJSON(stdout, struct {
			daemon.Result
			Pushed *bool `json:"pushed,omitempty"`
		}{result, pushed})
	}
	printLive(stdout, result, server, len(result.Databases) == 0 || hasBucket(ctx, server))
	if result.Release == 1 {
		if status, err := quietStatus(ctx, server); err == nil && len(status.Watchers) == 0 {
			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, "  "+style.Muted.Render("Nobody watches "+server+" yet. If it dies at 3 a.m., your users tell you first."))
			fmt.Fprintln(stdout, "  "+command(stdout, build.Name+" watcher connect "+server)+"   "+style.Muted.Render("30 days free. No card."))
		}
		return offerClose(ctx, server, stdin, stdout)
	}
	return nil
}

// deployedApp is the app's name: --app, else the catalog app's name, else
// this folder's.
func deployedApp(appName, catalogApp string) (string, error) {
	if appName == "" && catalogApp != "" {
		appName = catalogApp
	}
	if appName == "" {
		return folderApp()
	}
	return appName, checkApp(appName)
}

// deployServer is where the deploy goes: the server named, else the one
// that runs the app already. The first deploy picks one of your servers
// on a terminal.
func deployServer(ctx context.Context, flags *flagSet, app string, stdin io.Reader) (string, error) {
	if flags.NArg() > 1 {
		return "", flags.wrong()
	}
	if flags.NArg() == 1 {
		return flags.Arg(0), nil
	}
	return appServer(ctx, flags, app, stdin, fmt.Errorf("where to? The first deploy of %s names the server: `%s deploy <server>`, a name from your ssh config or user@host", app, build.Name))
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
		fmt.Fprintln(stdout, style.Muted.Render("Changes that are not committed stay here: "+build.Name+" sends "+commit+"."))
	}
	if image != "" {
		return commit + " with image " + image, nil
	}
	return commit, nil
}

// onRemote says whether the upstream of this branch has HEAD, and names
// its remote, like origin. Without an upstream, HEAD is on none: the
// remote named is the first one.
func onRemote(ctx context.Context) (remote string, ok bool) {
	branch, _ := gitOutput(ctx, "branch", "--show-current")
	if remote, _ := gitOutput(ctx, "config", "branch."+branch+".remote"); remote != "" {
		return remote, exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", "HEAD", "@{upstream}").Run() == nil
	}
	remotes, _ := gitOutput(ctx, "remote")
	if remote, _, _ = strings.Cut(remotes, "\n"); remote == "" {
		remote = "a remote"
	}
	return remote, false
}

// runDeploy runs the deploy on the server and shows its log as it comes.
func runDeploy(ctx context.Context, server, app, image, catalogApp string, noCopy bool, stdout io.Writer) (daemon.Result, error) {
	args := []string{"deploy", app}
	if noCopy {
		args = append(args, "--no-copy")
	}
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
		// Closed first: git ends even when the server stops reading early.
		defer pipe.Close()
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

// hasBucket reports whether the server has a bucket for its copies. When
// it does not answer, it says yes: no hint is better than a wrong one.
func hasBucket(ctx context.Context, server string) bool {
	var bucket *daemon.Bucket
	err := sshJSON(ctx, server, []string{"bucket"}, nil, &bucket)
	return err != nil || bucket != nil
}

// printLive says the app is live, with its address and what to do next:
// with a database, its backups, or the bucket they need first.
func printLive(stdout io.Writer, result daemon.Result, server string, bucket bool) {
	fmt.Fprintln(stdout)
	if len(result.Addresses) == 0 {
		done(stdout, "%s v%d runs. It has no domain yet: `%s domains add %s <domain>`", result.App, result.Release, build.Name, result.App)
		return
	}
	done(stdout, "%s v%d is live: %s", result.App, result.Release, result.Addresses[0])
	for _, address := range result.Addresses[1:] {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("also "+address))
	}
	switch {
	case len(result.Databases) == 0:
	case !bucket:
		hint(stdout, strings.Join(result.Databases, " and ")+" found. No backup yet:", build.Name+" bucket set "+server)
	default:
		hint(stdout, strings.Join(result.Databases, " and ")+" found. See its backups:", build.Name+" backups "+result.App)
	}
}

// sendImage puts a local image on the server: docker save here, docker
// load there.
func sendImage(ctx context.Context, server, image string) error {
	save := exec.CommandContext(ctx, "docker", "save", "--", image)
	save.Stderr = os.Stderr
	pipe, err := save.StdoutPipe()
	if err != nil {
		return err
	}
	if err := save.Start(); err != nil {
		return fmt.Errorf("docker save %s: %w", image, err)
	}
	loadErr := sshRun(ctx, server, shellJoin([]string{"docker", "load"}), pipe, io.Discard, os.Stderr)
	// Closed first, so docker save ends even when the server stopped
	// reading early.
	pipe.Close()
	saveErr := save.Wait()
	if loadErr != nil {
		return loadErr
	}
	if saveErr != nil {
		return fmt.Errorf("docker save %s: %w. Is the image on this computer? See `docker images`", image, saveErr)
	}
	return nil
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
	if oldDaemon(info.version) {
		return oldDaemonError(server, info.version)
	}
	if info.docker && info.version != "" {
		warnOtherVersion(stdout, server, info.version)
		return nil
	}
	if info.arch == "" {
		return fmt.Errorf("%s is not a 64-bit Intel or ARM server, which %s needs", server, build.Name)
	}
	if info.version == "" && !daemon.ValidServerName(serverName(server)) {
		return fmt.Errorf("the server calls itself %q for its backups and watchers, and that cannot be a name. Give it one in your ssh config, like Host my-server, and deploy to my-server", serverName(server))
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
	err = task(stdout, "installing "+build.Name+" "+build.Version, func() error {
		return installMoin(ctx, server, info.arch)
	})
	if err != nil {
		return err
	}
	return task(stdout, "starting the daemon: backups, checks, reports", func() error {
		return sshRun(ctx, server, hereLine("setup", serverName(server)), nil, io.Discard, os.Stderr)
	})
}

// lastOldVersion is the last release whose daemon took its commands from
// elsewhere, not over SSH.
const lastOldVersion = "v1.0.0-beta.37"

// oldDaemon reports whether version is that release or one before it.
func oldDaemon(version string) bool {
	return build.IsRelease(version) && !build.NewerVersion(version, lastOldVersion)
}

// oldDaemonError says how to remove the daemon of an old release, which
// takes no commands over SSH.
func oldDaemonError(server, version string) error {
	return fmt.Errorf("%s still runs the daemon of %s %s, which takes no commands over SSH. Remove it first, the apps keep running: ssh -t %s %s daemon uninstall. The Upgrade steps of beta.38 in the changelog say what follows", server, build.Name, version, server, build.Name)
}

// serverName is the name the server gets on its own, for its backups and
// its watchers: my-server for my-server, the host for user@host.
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
		return nil, fmt.Errorf("this build of %s is %s/%s and cannot run on a linux/%s server. Use a release, or build it for linux/%s", build.Name, runtime.GOOS, runtime.GOARCH, arch, arch)
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
