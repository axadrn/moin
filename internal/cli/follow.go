package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

// follow is `moin follow [app] --branch main`: the server deploys each
// new commit of the branch, read with a deploy key made on the server.
// `moin follow [app] --image ghcr.io/you/my-app:main` deploys each new
// image under that tag instead. `--off` stops it.
func follow(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("follow", "follow [app] [--branch main] [--repo <git url>] [--image <image>] [--off] [-s server]").withServer()
	branch := flags.String("branch", "", "the branch to deploy; default the branch of this folder")
	repo := flags.String("repo", "", "the repo; default the origin of this folder")
	image := flags.String("image", "", "deploy each new image under this tag, like ghcr.io/you/my-app:main, instead of a branch")
	off := flags.Bool("off", false, "stop following, and remove the deploy key from the server")
	if err := flags.parse(args); err != nil {
		return err
	}
	app, _, err := appArgs(flags, 0)
	if err != nil {
		return err
	}
	if *off {
		return onApp(ctx, flags, app, func(server string) error { return unfollow(ctx, server, app, stdout) })
	}
	if *image != "" {
		if *branch != "" || *repo != "" {
			return errors.New("follow takes --image or a branch, not both")
		}
		return onApp(ctx, flags, app, func(server string) error { return startFollowImage(ctx, server, app, *image, stdout) })
	}
	// This folder says the branch and the repo, when it is the app's.
	here, _ := folderApp()
	if *branch == "" && here == app {
		*branch, _ = gitOutput(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	}
	if *branch == "" || *branch == "HEAD" {
		*branch = "main"
	}
	if *repo == "" && here == app {
		*repo, _ = gitOutput(ctx, "remote", "get-url", "origin")
	}
	if *repo == "" {
		if !isTerminal(stdin) {
			return fmt.Errorf("which repo does %s follow? Add --repo git@github.com:you/%s.git", app, app)
		}
		if *repo, err = ask(stdin, stdout, "The git repo of "+app+", like git@github.com:you/"+app+".git:", ""); err != nil {
			return err
		}
		if *repo == "" {
			return errors.New("no repo, nothing changed")
		}
	}
	return onApp(ctx, flags, app, func(server string) error {
		return startFollow(ctx, server, app, sshRepo(*repo), *branch, stdin, stdout)
	})
}

// startFollow sets the follow on the server, puts its deploy key in the
// repo and reads the branch once.
func startFollow(ctx context.Context, server, app, repo, branch string, stdin io.Reader, stdout io.Writer) error {
	body, _ := json.Marshal(map[string]string{"repo": repo, "branch": branch})
	var f daemon.Follow
	if err := sshJSON(ctx, server, []string{"follow", app}, strings.NewReader(string(body)), &f); err != nil {
		return err
	}
	if !addDeployKey(ctx, server, repo, f.PublicKey, stdout) {
		fmt.Fprintln(stdout, "Add this read-only deploy key to "+repo+":")
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "  "+f.PublicKey)
		fmt.Fprintln(stdout)
		if owner := githubRepo(repo); owner != "" {
			fmt.Fprintln(stdout, style.Muted.Render("On GitHub: https://github.com/"+owner+"/settings/keys/new. Leave Allow write access off."))
		}
		if isTerminal(stdin) {
			fmt.Fprint(stdout, style.Warning.Render("?")+" Press Enter when it is added ")
			if _, err := readLine(stdin); err != nil {
				return err
			}
		}
	}
	var check struct {
		Head string `json:"head"`
	}
	err := sshJSON(ctx, server, []string{"follow", "check", app}, nil, &check)
	if err != nil {
		// The server said why already, unless ssh failed.
		var said exitCode
		if !errors.As(err, &said) {
			printError(stdout, err)
		}
		hint(stdout, server+" tries again every minute, and alerts once while it cannot read it. Stop with", build.Name+" follow "+app+" --off")
		return errReported
	}
	done(stdout, "%s follows %s. It deploys %s within a minute, then each new commit", app, branch, check.Head[:min(len(check.Head), 7)])
	fmt.Fprintln(stdout, "  "+style.Muted.Render("Whoever can push to "+branch+" deploys to "+server+". A deploy that fails alerts."))
	return nil
}

// addDeployKey adds the key to the repo with gh, when gh is installed
// and logged in and the repo is on GitHub. It says whether it did.
func addDeployKey(ctx context.Context, server, repo, key string, stdout io.Writer) bool {
	owner := githubRepo(repo)
	if owner == "" || exec.CommandContext(ctx, "gh", "auth", "status").Run() != nil {
		return false
	}
	file, err := os.CreateTemp("", build.Name+"-deploy-key-*.pub")
	if err != nil {
		return false
	}
	defer os.Remove(file.Name())
	_, err = file.WriteString(key + "\n")
	if errors.Join(err, file.Close()) != nil {
		return false
	}
	step(stdout, "adding a read-only deploy key to %s with gh", owner)
	add := exec.CommandContext(ctx, "gh", "repo", "deploy-key", "add", file.Name(), "--repo", owner, "--title", build.Name+" on "+server)
	if out, err := add.CombinedOutput(); err != nil {
		fmt.Fprintln(stdout, style.Muted.Render("gh did not add it: "+strings.TrimSpace(string(out))))
		return false
	}
	return true
}

// unfollow stops the follow and says to remove the deploy key.
func unfollow(ctx context.Context, server, app string, stdout io.Writer) error {
	var f daemon.Follow
	if err := sshJSON(ctx, server, []string{"follow", "off", app}, nil, &f); err != nil {
		return err
	}
	if f.Image != "" {
		done(stdout, "%s no longer follows %s", app, f.Image)
		return nil
	}
	done(stdout, "%s no longer follows %s. The deploy key is gone from %s", app, f.Branch, server)
	if owner := githubRepo(f.Repo); owner != "" {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("Remove it from the repo too: https://github.com/"+owner+"/settings/keys"))
	}
	return nil
}

// httpsRepo is a repo address over HTTPS, like https://github.com/you/my-app.
var httpsRepo = regexp.MustCompile(`^https://([^/@]+)/(.+?)(\.git)?/?$`)

// sshRepo is the SSH address of a repo given over HTTPS: a deploy key
// only works over SSH. Any other address stays.
func sshRepo(repo string) string {
	match := httpsRepo.FindStringSubmatch(repo)
	if match == nil {
		return repo
	}
	return "git@" + match[1] + ":" + match[2] + ".git"
}

// githubRepo is owner/name of a repo on GitHub, "" for any other.
func githubRepo(repo string) string {
	for _, prefix := range []string{"git@github.com:", "ssh://git@github.com/", "https://github.com/"} {
		if rest, ok := strings.CutPrefix(repo, prefix); ok {
			return strings.TrimSuffix(strings.TrimSuffix(rest, "/"), ".git")
		}
	}
	return ""
}

// hereFollow is `here follow <app>` with the repo and the branch, or the
// image, as JSON on stdin, `here follow check <app>` and `here follow off <app>`.
func hereFollow(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	dir := daemon.StateDir()
	switch {
	case len(args) == 1:
		var want struct{ Repo, Branch, Image string }
		if err := json.NewDecoder(stdin).Decode(&want); err != nil {
			return fmt.Errorf("read the repo: %w", err)
		}
		if want.Image != "" {
			f, err := daemon.FollowImageHere(dir, args[0], want.Image)
			return respond(stdout, f, err)
		}
		f, err := daemon.FollowHere(dir, args[0], want.Repo, want.Branch)
		return respond(stdout, f, err)
	case len(args) == 2 && args[0] == "check":
		head, err := daemon.CheckFollowHere(ctx, dir, args[1])
		return respond(stdout, map[string]string{"head": head}, err)
	case len(args) == 2 && args[0] == "off":
		f, err := daemon.UnfollowHere(dir, args[1])
		return respond(stdout, f, err)
	}
	return errors.New("usage: here follow <app> | check <app> | off <app>")
}
