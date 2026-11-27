package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/internal/alert"
)

// fakeFollower is a follower whose branch is at head and whose deploys
// end with fail, and that counts what it did.
type fakeFollower struct {
	head     string
	readFail error
	fail     error
	deployed []string
	alerts   []alert.Message
}

func (fake *fakeFollower) follower() *follower {
	return &follower{
		head: func(context.Context, Follow) (string, error) { return fake.head, fake.readFail },
		deploy: func(_ context.Context, f Follow) (string, error) {
			fake.deployed = append(fake.deployed, fake.head)
			return fake.head, fake.fail
		},
		alert: func(_ context.Context, message alert.Message) { fake.alerts = append(fake.alerts, message) },
	}
}

// A new commit deploys, the same commit again does nothing.
func TestFollowDeploysEachNewCommitOnce(t *testing.T) {
	fake := &fakeFollower{head: "aaa1111"}
	fw := fake.follower()
	f := fw.look(t.Context(), Follow{App: "shop", Branch: "main"})
	f = fw.look(t.Context(), f)
	if f.Commit != "aaa1111" || len(fake.deployed) != 1 || len(fake.alerts) != 0 {
		t.Fatalf("after two looks at one commit: %+v, deployed %q, alerts %v", f, fake.deployed, fake.alerts)
	}
	fake.head = "bbb2222"
	f = fw.look(t.Context(), f)
	if f.Commit != "bbb2222" || len(fake.deployed) != 2 {
		t.Fatalf("a new commit: %+v, deployed %q", f, fake.deployed)
	}
}

// A failed deploy alerts once and is not tried again. The next commit
// that deploys says it is over.
func TestFollowAlertsAFailedDeployOnce(t *testing.T) {
	fake := &fakeFollower{head: "aaa1111", fail: errors.New("the build failed")}
	fw := fake.follower()
	f := fw.look(t.Context(), Follow{App: "shop", Branch: "main"})
	f = fw.look(t.Context(), f)
	if len(fake.deployed) != 1 || len(fake.alerts) != 1 || !strings.Contains(fake.alerts[0].Title, "the deploy of aaa1111 from main failed") || fake.alerts[0].Text != "the build failed" {
		t.Fatalf("deployed %q, alerts %+v", fake.deployed, fake.alerts)
	}
	fake.head, fake.fail = "bbb2222", nil
	f = fw.look(t.Context(), f)
	if f.Problem != "" || len(fake.alerts) != 2 || !fake.alerts[1].OK {
		t.Fatalf("after a good deploy: %+v, alerts %+v", f, fake.alerts)
	}
}

// A branch moin cannot read alerts once while it lasts, deploys nothing,
// and says when it is over.
func TestFollowAlertsABranchItCannotReadOnce(t *testing.T) {
	fake := &fakeFollower{readFail: errors.New("Permission denied (publickey)")}
	fw := fake.follower()
	f := Follow{App: "shop", Branch: "main", Commit: "aaa1111"}
	for range 3 {
		f = fw.look(t.Context(), f)
	}
	if len(fake.deployed) != 0 || len(fake.alerts) != 1 || !strings.Contains(fake.alerts[0].Title, "cannot read the branch main") {
		t.Fatalf("deployed %q, alerts %+v", fake.deployed, fake.alerts)
	}
	fake.readFail, fake.head = nil, "aaa1111"
	f = fw.look(t.Context(), f)
	if f.Problem != "" || len(fake.alerts) != 2 || !fake.alerts[1].OK || len(fake.deployed) != 0 {
		t.Fatalf("read again: %+v, alerts %+v, deployed %q", f, fake.alerts, fake.deployed)
	}
}

// The real git: ls-remote reads the newest commit of a branch in a repo
// on disk.
func TestFollowReadsTheBranchWithGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = repo
		command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=ada@example.com", "GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=ada@example.com")
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "index.js"), []byte("1"), 0o644)
	run("add", ".")
	run("commit", "-qm", "one")
	want := run("rev-parse", "HEAD")
	dir := t.TempDir()
	fw := newFollower(&apps{}, dir)
	head, err := fw.head(t.Context(), Follow{App: "shop", Repo: "file://" + repo, Branch: "main"})
	if err != nil || head != want {
		t.Fatalf("head = %q, %v, want %q", head, err, want)
	}
	if _, err := fw.head(t.Context(), Follow{App: "shop", Repo: "file://" + repo, Branch: "nope"}); err == nil || !strings.Contains(err.Error(), "no branch nope") {
		t.Fatalf("a missing branch: %v", err)
	}
}

func TestValidBranch(t *testing.T) {
	for name, want := range map[string]bool{"main": true, "release/v2": true, "": false, "--upload-pack=x": false, "a..b": false, "a b": false} {
		if validBranch(name) != want {
			t.Errorf("validBranch(%q) = %v", name, !want)
		}
	}
}
