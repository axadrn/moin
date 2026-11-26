package client

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/axadrn/moin/internal/build"
)

// PushedCommit is what `moin deploy` deploys from dir: the GitHub repo of
// its remote origin, as owner/name, and the commit HEAD is at. The server
// builds that commit from GitHub, so it fails, saying what to do, unless dir
// is the top folder of a GitHub repo with every change committed and pushed.
func PushedCommit(dir string) (repo, commit string, err error) {
	repo = OriginRepo(dir)
	if repo == "" {
		return "", "", fmt.Errorf("%s deploys from GitHub, a CI image or the catalog: push this folder to a GitHub repo and run %s deploy again, or deploy an image with --image", build.Name, build.Name)
	}
	if prefix, _ := git(dir, "rev-parse", "--show-prefix"); prefix != "" {
		top, _ := git(dir, "rev-parse", "--show-toplevel")
		return "", "", fmt.Errorf("%s deploys the whole repo %s: run %s deploy in its top folder, %s", build.Name, repo, build.Name, top)
	}
	changes, err := git(dir, "status", "--porcelain")
	if err != nil {
		return "", "", err
	}
	if changes != "" {
		return "", "", fmt.Errorf("this folder has changes that are not committed, and %s deploys what is on GitHub: commit and push them, then run %s deploy again", build.Name, build.Name)
	}
	commit, err = git(dir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("%s has no commit yet: commit, push and run %s deploy again", repo, build.Name)
	}
	pushed, err := git(dir, "for-each-ref", "--contains", commit, "--format=%(refname)", "refs/remotes/origin")
	if err != nil {
		return "", "", err
	}
	if pushed == "" {
		return "", "", fmt.Errorf("commit %s is not on GitHub yet: run git push, then %s deploy again", commit[:min(7, len(commit))], build.Name)
	}
	return repo, commit, nil
}

// git runs git in dir and returns what it printed, trimmed.
func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// gitHubRemote is a remote on GitHub, over HTTPS or SSH.
var gitHubRemote = regexp.MustCompile(`^(?:https://(?:[^@/]+@)?github\.com/|git@github\.com:|ssh://git@github\.com/)([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// OriginRepo is owner/name of the GitHub repo of dir's remote origin, ""
// when it has none.
func OriginRepo(dir string) string {
	origin, err := git(dir, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return gitHubRepo(origin)
}

// gitHubRepo is owner/name of a GitHub remote URL, or "" for any other.
func gitHubRepo(remote string) string {
	match := gitHubRemote.FindStringSubmatch(remote)
	if match == nil {
		return ""
	}
	return match[1]
}

var unsafeName = regexp.MustCompile(`[^a-z0-9-]+`)

// AppName picks the app: the flag, or the name of the directory.
func AppName(flagValue, dir string) string {
	name, _ := PickApp(flagValue, dir)
	return name
}

// PickApp is AppName that also says where the name came from: "--app" or
// FromDirectory.
func PickApp(flagValue, dir string) (name, from string) {
	if flagValue != "" {
		return flagValue, "--app"
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", ""
	}
	return strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(filepath.Base(absolute)), "-"), "-"), FromDirectory
}

// FromDirectory says an app name came from the name of the directory.
const FromDirectory = "the directory name"
