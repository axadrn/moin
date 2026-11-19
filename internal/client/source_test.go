package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadLocalConfig(t *testing.T) {
	dir := t.TempDir()
	compose := "services:\n  web:\n    build: .\n\nx-moin:\n  app: blog # the name\n  server: \"web-1\"\n  domains: [blog.example.com]\n\nvolumes:\n  app: {}\n"
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte(compose), 0o644)
	config := ReadLocalConfig(dir)
	if config.App != "blog" || config.Server != "web-1" {
		t.Fatalf("config = %+v", config)
	}
	// The block of the old name counts for nothing: the daemon refuses it.
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("x-ops:\n  app: old\n"), 0o644)
	if got := ReadLocalConfig(dir); got.App != "" {
		t.Fatalf("x-ops read: %+v", got)
	}
	if got := ReadLocalConfig(t.TempDir()); got != (LocalConfig{}) {
		t.Fatalf("no compose.yml: %+v", got)
	}
}

// pushedRepo is a repo with one commit, origin on GitHub, and that commit
// on origin's main as git push leaves it.
func pushedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("services: {}\n"), 0o644)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"remote", "add", "origin", "git@github.com:me/shop.git"},
		{"add", "."},
		{"-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "first"},
		{"update-ref", "refs/remotes/origin/main", "HEAD"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

// moin deploy deploys the pushed commit of a GitHub repo and says what to
// do for everything else.
func TestPushedCommit(t *testing.T) {
	dir := pushedRepo(t)
	head, _ := git(dir, "rev-parse", "HEAD")
	if repo, commit, err := PushedCommit(dir); err != nil || repo != "me/shop" || commit != head {
		t.Fatalf("pushed = %s %s, %v", repo, commit, err)
	}
	fails := func(want string) {
		t.Helper()
		if _, _, err := PushedCommit(dir); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644)
	fails("not committed")
	git(dir, "add", ".")
	git(dir, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "second")
	fails("is not on GitHub yet: run git push")
	os.Mkdir(filepath.Join(dir, "web"), 0o755)
	_, _, err := PushedCommit(filepath.Join(dir, "web"))
	if err == nil || !strings.Contains(err.Error(), "top folder") {
		t.Errorf("a subfolder: %v", err)
	}
	git(dir, "remote", "set-url", "origin", "https://gitlab.com/me/shop.git")
	fails("push this folder to a GitHub repo")
	if _, _, err := PushedCommit(t.TempDir()); err == nil || !strings.Contains(err.Error(), "push this folder to a GitHub repo") {
		t.Errorf("no repo: %v", err)
	}
}

func TestGitHubRepo(t *testing.T) {
	for remote, want := range map[string]string{
		"https://github.com/me/shop.git":            "me/shop",
		"https://github.com/me/shop":                "me/shop",
		"https://x-access-token@github.com/me/shop": "me/shop",
		"git@github.com:me/my.shop.git":             "me/my.shop",
		"ssh://git@github.com/me/shop.git":          "me/shop",
		"https://gitlab.com/me/shop.git":            "",
		"https://github.com.evil.example/me/shop":   "",
		"git@github.com:me/shop/extra.git":          "",
	} {
		if got := gitHubRepo(remote); got != want {
			t.Errorf("%s = %q, want %q", remote, got, want)
		}
	}
}
