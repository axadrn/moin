package cli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
)

// TestShellJoin: every word reaches the server as it is, quotes and all.
func TestShellJoin(t *testing.T) {
	words := []string{"psql", "-c", "select 'a'; drop table x", "$HOME", "it's"}
	out, err := exec.Command("sh", "-c", "printf '%s\\n' "+shellJoin(words)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if want := "psql\n-c\nselect 'a'; drop table x\n$HOME\nit's\n"; string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

// TestGoArch knows the machines moin has builds for.
func TestGoArch(t *testing.T) {
	for machine, want := range map[string]string{"x86_64": "amd64", "aarch64\n": "arm64", "armv7l": ""} {
		if got := goArch(machine); got != want {
			t.Errorf("goArch(%q) = %q, want %q", machine, got, want)
		}
	}
}

// TestOldDaemon: beta.37 and before have to go before a deploy; a newer
// release and a build without one do not.
func TestOldDaemon(t *testing.T) {
	for version, want := range map[string]bool{"v1.0.0-beta.37": true, "v1.0.0-beta.9": true, "v1.0.0-beta.38": false, "v1.0.0": false, "dev": false, "": false} {
		if got := oldDaemon(version); got != want {
			t.Errorf("oldDaemon(%q) = %v, want %v", version, got, want)
		}
	}
}

// TestServerName drops the user of user@host.
func TestServerName(t *testing.T) {
	for server, want := range map[string]string{"prod": "prod", "root@203.0.113.7": "203.0.113.7"} {
		if got := serverName(server); got != want {
			t.Errorf("serverName(%q) = %q, want %q", server, got, want)
		}
	}
}

// TestReleaseNumber reads v3 and 3, nothing else.
func TestReleaseNumber(t *testing.T) {
	for text, want := range map[string]int{"v3": 3, "12": 12} {
		if got, err := releaseNumber(text); err != nil || got != want {
			t.Errorf("releaseNumber(%q) = %d, %v", text, got, err)
		}
	}
	for _, text := range []string{"v0", "three", ""} {
		if _, err := releaseNumber(text); err == nil {
			t.Errorf("releaseNumber(%q): want an error", text)
		}
	}
}

// TestCatalog lists the apps built into moin, each with its pitch.
func TestCatalog(t *testing.T) {
	entries, err := catalogEntries()
	if err != nil || len(entries) == 0 {
		t.Fatalf("catalog = %d apps, %v", len(entries), err)
	}
	for _, entry := range entries {
		if entry.Title == "" || entry.Pitch == "" {
			t.Errorf("%s has no name or pitch in its app.md", entry.Name)
		}
	}
}

// TestPickRelease: Enter takes the one before the live one, a typed one
// wins.
func TestPickRelease(t *testing.T) {
	releases := []daemon.Release{{Number: 5, Live: true}, {Number: 4}, {Number: 3, Before: true}}
	for answer, want := range map[string]int{"\n": 3, "v4\n": 4, "4\n": 4} {
		got, err := pickRelease(strings.NewReader(answer), io.Discard, "shop", releases, time.Now())
		if err != nil || got != want {
			t.Errorf("answer %q: v%d, %v; want v%d", answer, got, err, want)
		}
	}
	if _, err := pickRelease(strings.NewReader("\n"), io.Discard, "shop", releases[:1], time.Now()); err == nil {
		t.Error("only the live release: want an error")
	}
}

// TestIsPublicKey takes one line of a .pub file, never a private key.
func TestIsPublicKey(t *testing.T) {
	for body, want := range map[string]bool{
		"ssh-ed25519 AAAAC3Nz ada@laptop\n":                           true,
		"ecdsa-sha2-nistp256 AAAAE2 ada\n":                            true,
		"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEA\n": false,
		"ssh-ed25519 AAAA a\nssh-ed25519 BBBB b\n":                    false,
	} {
		if got := isPublicKey([]byte(body)); got != want {
			t.Errorf("isPublicKey(%q) = %v, want %v", body, got, want)
		}
	}
}

// TestDeployedApp: --app, then the catalog app, then the folder.
func TestDeployedApp(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, test := range [][3]string{{"blog", "umami", "blog"}, {"", "umami", "umami"}} {
		if got, err := deployedApp(test[0], test[1]); err != nil || got != test[2] {
			t.Errorf("deployedApp(%q, %q) = %q, %v", test[0], test[1], got, err)
		}
	}
	if _, err := deployedApp("Not_An_App", ""); err == nil {
		t.Error("a bad --app: want an error")
	}
}

// TestEditUntilValid opens the editor again after a mistake and keeps the
// fixed file.
func TestEditUntilValid(t *testing.T) {
	dir := t.TempDir()
	editor := filepath.Join(dir, "editor.sh")
	script := "#!/bin/sh\nif [ -e " + dir + "/once ]; then echo 'A=\"fixed\"' > \"$1\"; else touch " + dir + "/once; echo 'A=\"broken' > \"$1\"; fi\n"
	if err := os.WriteFile(editor, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)
	got, err := editUntilValid(t.Context(), "shop", []byte("A=\"1\"\n"), strings.NewReader(""), io.Discard)
	if err != nil || string(got) != "A=\"fixed\"\n" {
		t.Errorf("got %q, %v", got, err)
	}
}

// TestEditUntilValidGivesUp when the editor saves the same mistake again.
func TestEditUntilValidGivesUp(t *testing.T) {
	editor := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\necho 'A=\"broken' > \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)
	if _, err := editUntilValid(t.Context(), "shop", []byte("A=\"1\"\n"), strings.NewReader(""), io.Discard); err == nil {
		t.Error("the same mistake twice: want an error")
	}
}

// fakeSSH puts an ssh in PATH that notes its arguments, one line per
// run, in the file it returns, and answers out.
func fakeSSH(t *testing.T, out string) string {
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	fake := "#!/bin/sh\necho \"$@\" >> " + runs + "\nprintf '%s' '" + out + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return runs
}

// TestQuietJSONNeverAsks: many servers at once run ssh in batch mode, so
// none of them asks for a password on the terminal.
func TestQuietJSONNeverAsks(t *testing.T) {
	runs := fakeSSH(t, `{"total": 3}`)
	var answer struct {
		Total int `json:"total"`
	}
	if err := quietJSON(context.Background(), "prod", []string{"knocks"}, &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Total != 3 {
		t.Errorf("total = %d, want 3", answer.Total)
	}
	args, err := os.ReadFile(runs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(args), "-o BatchMode=yes ") {
		t.Errorf("ssh ran with %q, want BatchMode first", args)
	}
}

// TestUpdateStopsOnAnOldDaemon: the daemon of beta.37 knows no `moin
// here`, so update says how to remove it and runs nothing more.
func TestUpdateStopsOnAnOldDaemon(t *testing.T) {
	runs := fakeSSH(t, "x86_64\ndocker\n"+build.Name+" v1.0.0-beta.37\n")
	err := updateServer(context.Background(), "prod", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "daemon uninstall") {
		t.Fatalf("err = %v, want the uninstall hint", err)
	}
	args, _ := os.ReadFile(runs)
	if n := strings.Count(string(args), "\n"); n != 1 {
		t.Errorf("ssh ran %d times, want only the look", n)
	}
}
