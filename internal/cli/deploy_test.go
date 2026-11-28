package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/internal/daemon"
)

func TestPrintLiveSaysWhatTheBackupsNeed(t *testing.T) {
	result := daemon.Result{App: "shop", Release: 1, Addresses: []string{"https://shop.example.com"}, Databases: []string{"postgres"}}
	for _, test := range []struct {
		bucket    bool
		want, not string
	}{
		{false, "No backup yet: `moin bucket set prod`", "See its backups"},
		{true, "See its backups: `moin backups shop`", "No backup yet"},
	} {
		var out strings.Builder
		printLive(terminal(&out), result, "prod", test.bucket)
		if !strings.Contains(out.String(), test.want) || strings.Contains(out.String(), test.not) {
			t.Errorf("bucket %v: printLive wrote\n%s\nwant %q, not %q", test.bucket, out.String(), test.want, test.not)
		}
	}
}

func TestPrintLiveWithoutADatabaseSaysNothingOfBackups(t *testing.T) {
	var out strings.Builder
	printLive(terminal(&out), daemon.Result{App: "shop", Release: 1, Addresses: []string{"https://shop.example.com"}}, "prod", false)
	if strings.Contains(out.String(), "backup") {
		t.Errorf("printLive wrote\n%s\nwant nothing about backups", out.String())
	}
}

func TestNearCommand(t *testing.T) {
	for typed, want := range map[string]string{"deplyo": "deploy", "lgos": "logs", "backpus": "backups", "restor": "restore", "zebra": ""} {
		if got := nearCommand(typed); got != want {
			t.Errorf("nearCommand(%q) = %q, want %q", typed, got, want)
		}
	}
}

func TestOnRemote(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	bare := filepath.Join(dir, "origin.git")
	t.Chdir(t.TempDir())
	git := func(args ...string) {
		t.Helper()
		args = append([]string{"-c", "user.name=Ada", "-c", "user.email=ada@example.com"}, args...)
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	check := func(wantRemote string, wantOK bool) {
		t.Helper()
		if remote, ok := onRemote(t.Context()); remote != wantRemote || ok != wantOK {
			t.Errorf("onRemote() = %q, %v, want %q, %v", remote, ok, wantRemote, wantOK)
		}
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "--no-verify", "-m", "one")
	check("a remote", false)

	git("init", "-q", "--bare", bare)
	git("remote", "add", "origin", bare)
	check("origin", false)

	git("push", "-q", "--no-verify", "-u", "origin", "main")
	check("origin", true)

	git("commit", "-q", "--allow-empty", "--no-verify", "-m", "two")
	check("origin", false)
}

// --domain is for a catalog app: your own app gets one with domains add.
func TestDeployDomainNeedsCatalog(t *testing.T) {
	err := deploy(t.Context(), []string{"prod", "--domain", "shop.example.com"}, strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--domain goes with --catalog") {
		t.Errorf("err = %v", err)
	}
}
