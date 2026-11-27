package daemon

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/build"
)

// `moin follow` makes the server deploy each new commit of a branch. The
// daemon asks the repo every minute, with `git ls-remote` and a read-only
// deploy key made on the server. A new commit is cloned and deployed the
// way a deploy from your laptop is. Each commit is tried once; a deploy
// that fails alerts. CI needs no key to the server: whoever can push to
// the branch deploys.
//
// Each app that follows has a folder <state dir>/follow/<app>:
//
//	follow.json   the repo, the branch and the newest commit tried
//	key, key.pub  the deploy key
//	known_hosts   the repo's host key, kept on first contact

const followEvery = time.Minute

// Follow is an app that deploys a branch.
type Follow struct {
	App    string `json:"app"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	// Commit is the newest commit moin tried, deployed or failed.
	Commit string `json:"commit,omitempty"`
	// Problem is the title of what went wrong last, alerted once. Empty
	// while all is well.
	Problem string `json:"problem,omitempty"`
	// PublicKey is the deploy key to add to the repo, for the CLI. It is
	// kept in key.pub.
	PublicKey string `json:"public_key,omitempty"`
}

func followDir(dir, app string) string { return filepath.Join(dir, "follow", app) }

func loadFollow(dir, app string) (Follow, error) {
	var f Follow
	body, err := os.ReadFile(filepath.Join(followDir(dir, app), "follow.json"))
	if err != nil {
		return f, err
	}
	return f, json.Unmarshal(body, &f)
}

func saveFollow(dir string, f Follow) error {
	f.PublicKey = ""
	return writeState(filepath.Join(followDir(dir, f.App), "follow.json"), f)
}

// FollowHere makes app follow branch of repo. It makes the deploy key
// the first time and returns its public half.
func FollowHere(dir, app, repo, branch string) (Follow, error) {
	if err := hereApps(dir).mustHave(app); err != nil {
		return Follow{}, err
	}
	if repo == "" || strings.HasPrefix(repo, "-") {
		return Follow{}, fmt.Errorf("%q is not a git repo", repo)
	}
	if !validBranch(branch) {
		return Follow{}, fmt.Errorf("%q is not a branch name", branch)
	}
	f, err := loadFollow(dir, app)
	if err != nil || f.Repo != repo || f.Branch != branch {
		f = Follow{App: app, Repo: repo, Branch: branch}
	}
	key := filepath.Join(followDir(dir, app), "key")
	if _, err := os.Stat(key); err != nil {
		if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
			return Follow{}, err
		}
		comment := build.Name + " " + app
		if config, err := LoadConfig(); err == nil && config.Name != "" {
			comment += " on " + config.Name
		}
		output, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", comment, "-f", key).CombinedOutput()
		if err != nil {
			return Follow{}, fmt.Errorf("make the deploy key: %s", strings.TrimSpace(string(output)))
		}
	}
	if err := saveFollow(dir, f); err != nil {
		return Follow{}, err
	}
	public, err := os.ReadFile(key + ".pub")
	f.PublicKey = strings.TrimSpace(string(public))
	return f, err
}

// CheckFollowHere reads the branch once, with the deploy key, and says
// its newest commit.
func CheckFollowHere(ctx context.Context, dir, app string) (string, error) {
	f, err := loadFollow(dir, app)
	if err != nil {
		return "", fmt.Errorf("%s follows no branch", app)
	}
	return newFollower(hereApps(dir), dir).head(ctx, f)
}

// UnfollowHere stops app from following, and removes its deploy key.
func UnfollowHere(dir, app string) (Follow, error) {
	f, err := loadFollow(dir, app)
	if err != nil {
		return Follow{}, fmt.Errorf("%s follows no branch", app)
	}
	return f, os.RemoveAll(followDir(dir, app))
}

// validBranch is a branch name git takes and no option.
func validBranch(name string) bool {
	return name != "" && !strings.HasPrefix(name, "-") && !strings.ContainsAny(name, " \t\n~^:?*[\\") && !strings.Contains(name, "..")
}

// follower looks at the branches. head and deploy do the git and the
// deploy, alert says what went wrong; tests give their own.
type follower struct {
	dir    string
	head   func(ctx context.Context, f Follow) (string, error)
	deploy func(ctx context.Context, f Follow) (string, error)
	alert  func(ctx context.Context, message alert.Message)
}

func newFollower(a *apps, dir string) *follower {
	fw := &follower{dir: dir}
	fw.head = func(ctx context.Context, f Follow) (string, error) {
		out, err := fw.git(ctx, f, "ls-remote", "--", f.Repo, "refs/heads/"+f.Branch)
		if err != nil {
			return "", err
		}
		commit, _, _ := strings.Cut(out, "\t")
		if commit == "" {
			return "", fmt.Errorf("%s has no branch %s", f.Repo, f.Branch)
		}
		return commit, nil
	}
	fw.deploy = func(ctx context.Context, f Follow) (string, error) {
		return fw.deployBranch(ctx, a, f)
	}
	fw.alert = func(ctx context.Context, message alert.Message) {
		slog.Warn("alert", "title", message.Title, "text", message.Text)
		if config, err := LoadConfig(); err == nil {
			message.Server = config.Name
			sendAlert(ctx, config, message)
		}
	}
	return fw
}

// loop looks at every branch now and every followEvery, until ctx ends.
func (fw *follower) loop(ctx context.Context) {
	ticker := time.NewTicker(followEvery)
	defer ticker.Stop()
	for {
		fw.round(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// round looks at each app that follows a branch, one after the other.
func (fw *follower) round(ctx context.Context) {
	entries, _ := os.ReadDir(filepath.Join(fw.dir, "follow"))
	for _, entry := range entries {
		f, err := loadFollow(fw.dir, entry.Name())
		if err != nil {
			continue
		}
		next := fw.look(ctx, f)
		if next == f {
			continue
		}
		// Kept only when nobody changed the follow meanwhile, or
		// stopped it.
		now, err := loadFollow(fw.dir, f.App)
		if err != nil || now.Repo != f.Repo || now.Branch != f.Branch {
			continue
		}
		if err := saveFollow(fw.dir, next); err != nil {
			slog.Error("follow", "app", f.App, "error", err)
		}
	}
}

// look reads the branch once and deploys a commit it did not try yet.
func (fw *follower) look(ctx context.Context, f Follow) Follow {
	cannotRead := fmt.Sprintf("%s: %s cannot read the branch %s", f.App, build.Name, f.Branch)
	head, err := fw.head(ctx, f)
	if err != nil {
		return fw.problem(ctx, f, cannotRead, err)
	}
	if f.Problem == cannotRead {
		f = fw.over(ctx, f)
	}
	if head == f.Commit {
		return f
	}
	commit, err := fw.deploy(ctx, f)
	// A deploy that got no commit did not start, like a clone in a
	// network blip: the next look tries again.
	if commit != "" {
		f.Commit = commit
	}
	if err != nil {
		return fw.problem(ctx, f, fmt.Sprintf("%s: the deploy of %s from %s failed", f.App, short(cmp.Or(commit, head)), f.Branch), err)
	}
	slog.Info("follow: deployed", "app", f.App, "branch", f.Branch, "commit", short(f.Commit))
	if f.Problem != "" {
		f = fw.over(ctx, f)
	}
	return f
}

// problem alerts title once: a problem that lasts says nothing new.
func (fw *follower) problem(ctx context.Context, f Follow, title string, err error) Follow {
	if f.Problem != title {
		fw.alert(ctx, alert.Message{App: f.App, Title: title, Text: err.Error()})
		f.Problem = title
	}
	return f
}

// over alerts that the last problem is over.
func (fw *follower) over(ctx context.Context, f Follow) Follow {
	fw.alert(ctx, alert.Message{App: f.App, Title: "over: " + f.Problem, OK: true})
	f.Problem = ""
	return f
}

// deployBranch clones the branch and deploys it, as a deploy from your
// laptop does. It returns the commit it built.
func (fw *follower) deployBranch(ctx context.Context, a *apps, f Follow) (string, error) {
	work, err := os.MkdirTemp(followDir(fw.dir, f.App), "deploy-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	clone := filepath.Join(work, "src")
	if _, err := fw.git(ctx, f, "clone", "--quiet", "--depth=1", "--single-branch", "--branch="+f.Branch, "--", f.Repo, clone); err != nil {
		return "", err
	}
	commit, err := fw.git(ctx, f, "-C", clone, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	source := filepath.Join(work, "source.tar.gz")
	if _, err := fw.git(ctx, f, "-C", clone, "archive", "--format=tar.gz", "--output="+source, "HEAD"); err != nil {
		return commit, err
	}
	start := a.newStart(f.App)
	start.SourceFile = source
	var log bytes.Buffer
	_, err = a.deployHere(ctx, start, &log)
	if err != nil {
		return commit, fmt.Errorf("%w. See the releases with `%s rollback %s`", err, build.Name, f.App)
	}
	return commit, nil
}

// git runs git with the app's deploy key and nothing to ask, and returns
// what it printed.
func (fw *follower) git(ctx context.Context, f Follow, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	folder := followDir(fw.dir, f.App)
	ssh := "ssh -i " + shellQuote(filepath.Join(folder, "key")) +
		" -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new" +
		" -o UserKnownHostsFile=" + shellQuote(filepath.Join(folder, "known_hosts"))
	command := exec.CommandContext(ctx, "git", args...)
	// Only ssh: a repo URL like ext:: or file:// would run commands or
	// read files on the server.
	command.Env = append(os.Environ(), "GIT_SSH_COMMAND="+ssh, "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=ssh")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if errors.Is(err, exec.ErrNotFound) {
		return "", errors.New("git is not installed on the server: apt-get install git")
	}
	if err != nil {
		// git says why in a few lines, like "Permission denied
		// (publickey)" and then what to check: all of them, on one.
		return "", fmt.Errorf("git: %s", strings.Join(strings.Fields(cmp.Or(stderr.String(), err.Error())), " "))
	}
	return strings.TrimSpace(string(out)), nil
}

// shellQuote quotes a word for the shell git runs ssh in.
func shellQuote(word string) string {
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// short is the first 7 characters of a commit.
func short(commit string) string { return commit[:min(len(commit), 7)] }
