package daemon

import (
	"context"
	"errors"
	"os/exec"
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

// The real git: a repo that is not reached over ssh is refused, like a
// file:// URL that would read the server's own disk.
func TestFollowReadsOnlyOverSSH(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	command := exec.Command("git", "init", "-q", "-b", "main", repo)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", out)
	}
	fw := newFollower(&apps{}, t.TempDir())
	_, err := fw.head(t.Context(), Follow{App: "shop", Repo: "file://" + repo, Branch: "main"})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("a file:// repo: %v", err)
	}
}

// A deploy that got no commit, like a clone in a network blip, is tried
// again at the next look.
func TestFollowTriesAgainWhenTheDeployGotNoCommit(t *testing.T) {
	fake := &fakeFollower{head: "aaa1111"}
	fw := fake.follower()
	fw.deploy = func(context.Context, Follow) (string, error) {
		fake.deployed = append(fake.deployed, "")
		return "", errors.New("git: could not resolve host")
	}
	f := fw.look(t.Context(), Follow{App: "shop", Branch: "main"})
	f = fw.look(t.Context(), f)
	if f.Commit != "" || len(fake.deployed) != 2 || len(fake.alerts) != 1 {
		t.Fatalf("after two failed clones: %+v, deployed %q, alerts %+v", f, fake.deployed, fake.alerts)
	}
}

func TestValidBranch(t *testing.T) {
	for name, want := range map[string]bool{"main": true, "release/v2": true, "": false, "--upload-pack=x": false, "a..b": false, "a b": false} {
		if validBranch(name) != want {
			t.Errorf("validBranch(%q) = %v", name, !want)
		}
	}
}
