package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
	"time"

	"github.com/axadrn/moin/internal/alert"
	"github.com/axadrn/moin/internal/daemon"
)

// TestReadLineStopsOnCtrlC: Ctrl-C ends a question that waits for its
// answer, without Enter.
func TestReadLineStopsOnCtrlC(t *testing.T) {
	// The test catches Ctrl-C too, so a signal that comes before readLine
	// listens does not end the test.
	caught := make(chan os.Signal, 1)
	signal.Notify(caught, os.Interrupt)
	defer signal.Stop(caught)
	never, typed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer never.Close()
	defer typed.Close()
	got := make(chan error, 1)
	go func() {
		_, err := readLine(never)
		got <- err
	}()
	deadline := time.After(5 * time.Second)
	for {
		if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-got:
			if !errors.Is(err, errStopped) {
				t.Fatalf("err = %v, want errStopped", err)
			}
			return
		case <-deadline:
			t.Fatal("readLine still waits after Ctrl-C")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestReadLineReadsTheAnswer: without Ctrl-C, readLine gives the line.
func TestReadLineReadsTheAnswer(t *testing.T) {
	answer, typed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer answer.Close()
	if _, err := typed.WriteString("  yes \n"); err != nil {
		t.Fatal(err)
	}
	typed.Close()
	if line, err := readLine(answer); err != nil || line != "yes" {
		t.Fatalf("readLine = %q, %v", line, err)
	}
}

// typed is what you type on a terminal, line by line: each question reads
// its own line and no more.
func typed(lines ...string) io.Reader {
	return iotest.OneByteReader(strings.NewReader(strings.Join(lines, "\n") + "\n"))
}

// TestPick: a choice is picked by its number or its name, Enter takes the
// suggested one, anything else is an error.
func TestPick(t *testing.T) {
	for answer, want := range map[string]string{"2": "slack", "email": "email"} {
		if got, err := pick(typed(answer), io.Discard, "Where?", alertKinds, ""); err != nil || got != want {
			t.Errorf("pick %q = %q, %v, want %q", answer, got, err, want)
		}
	}
	if got, err := pickServer(typed(""), io.Discard, []string{"my-server"}); err != nil || got != "my-server" {
		t.Errorf("Enter with one server = %q, %v", got, err)
	}
	for _, answer := range []string{"", "9", "web-2"} {
		if got, err := pickServer(typed(answer), io.Discard, []string{"my-server", "web-1"}); err == nil {
			t.Errorf("pick %q = %q, want an error", answer, got)
		}
	}
}

// TestServerArgs: the server comes first. Left out without a terminal,
// the error is the usage line.
func TestServerArgs(t *testing.T) {
	flags := newFlags("keys add", "keys add <server> <file.pub>")
	if err := flags.parse([]string{"my-server", "anna.pub"}); err != nil {
		t.Fatal(err)
	}
	if server, rest, err := flags.serverArgs(strings.NewReader(""), 1); err != nil || server != "my-server" || len(rest) != 1 || rest[0] != "anna.pub" {
		t.Errorf("serverArgs = %q, %q, %v", server, rest, err)
	}
	if _, _, err := flags.serverArgs(strings.NewReader(""), 0); err == nil {
		t.Error("one argument too many: want an error")
	}
	if err := flags.parse(nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := flags.serverArgs(strings.NewReader(""), 1); err == nil || !strings.Contains(err.Error(), "keys add <server>") {
		t.Errorf("no server without a terminal = %v, want the usage line", err)
	}
}

// TestMissingWithoutTerminal: what a command needs and you left out is an
// error that says what to pass, when there is no terminal to ask on.
func TestMissingWithoutTerminal(t *testing.T) {
	fakeSSH(t, "")
	ctx := context.Background()
	cases := []struct {
		name string
		run  func() error
		want string
	}{
		{"keys add", func() error { return addKey(ctx, []string{"my-server"}, strings.NewReader(""), io.Discard) }, "<file.pub>"},
		{"keys", func() error { return keys(ctx, nil, strings.NewReader(""), io.Discard) }, "keys <server>"},
		{"registry set", func() error {
			return setRegistry(ctx, []string{"my-server", "ghcr.io"}, strings.NewReader(""), io.Discard)
		}, "<registry> <user>"},
		{"alerts add", func() error { return addAlert(ctx, []string{"my-server"}, strings.NewReader(""), io.Discard) }, "telegram, slack"},
		{"alerts add slack", func() error { return addAlert(ctx, []string{"my-server", "slack"}, strings.NewReader(""), io.Discard) }, "--url"},
		{"alerts add email", func() error {
			return addAlert(ctx, []string{"my-server", "email", "--to", "ada@example.com"}, strings.NewReader(""), io.Discard)
		}, "--smtp"},
		{"domains add", func() error { return changeDomain(ctx, "add", []string{"my-app"}, strings.NewReader(""), io.Discard) }, "<domain>"},
		{"watcher connect", func() error {
			return watcherConnect(ctx, []string{"my-server", "https://watcher.example.com"}, strings.NewReader(""), io.Discard)
		}, "no token"},
		{"servers rm", func() error { return removeServerCommand(nil, strings.NewReader(""), io.Discard) }, "servers rm <server>"},
	}
	for _, c := range cases {
		if err := c.run(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s = %v, want an error with %q", c.name, err, c.want)
		}
	}
}

// TestAskAlert: on a terminal, moin asks where alerts go and the values
// the flags left out. Who an email is from is a question Enter skips.
func TestAskAlert(t *testing.T) {
	var telegram alert.Destination
	if err := askAlert(&telegram, typed("1", "123:abc", "-42"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if want := (alert.Destination{Kind: "telegram", Token: "123:abc", ChatID: "-42"}); telegram != want {
		t.Errorf("telegram = %+v, want %+v", telegram, want)
	}
	email := alert.Destination{Kind: "email", To: "ada@example.com"}
	if err := askAlert(&email, typed("smtp.example.com:587", "ada", ""), io.Discard); err != nil {
		t.Fatal(err)
	}
	if want := (alert.Destination{Kind: "email", To: "ada@example.com", SMTP: "smtp.example.com:587", User: "ada"}); email != want {
		t.Errorf("email = %+v, want %+v", email, want)
	}
	email = alert.Destination{Kind: "email", To: "ada@example.com", SMTP: "smtp.example.com:587", User: "ada"}
	if err := askAlert(&email, typed("alerts@example.com"), io.Discard); err != nil || email.From != "alerts@example.com" {
		t.Errorf("from = %q, %v", email.From, err)
	}
	if err := askAlert(&alert.Destination{Kind: "pager"}, typed(), io.Discard); err == nil {
		t.Error("an unknown kind: want an error")
	}
	if err := askAlert(&alert.Destination{Kind: "slack"}, typed(""), io.Discard); err == nil {
		t.Error("no url: want an error")
	}
}

// TestAskLogin: the registry is asked, Enter takes ghcr.io, then the user.
func TestAskLogin(t *testing.T) {
	if got, err := askLogin(nil, typed("", "ada"), io.Discard); err != nil || got != (daemon.Registry{Registry: "ghcr.io", User: "ada"}) {
		t.Errorf("askLogin = %+v, %v", got, err)
	}
	if got, err := askLogin([]string{"registry.example.com"}, typed("ada"), io.Discard); err != nil || got != (daemon.Registry{Registry: "registry.example.com", User: "ada"}) {
		t.Errorf("askLogin with the registry = %+v, %v", got, err)
	}
	if _, err := askLogin(nil, typed("", ""), io.Discard); err == nil {
		t.Error("no user: want an error")
	}
}

// TestAskKeyFile: Enter takes ~/.ssh/id_ed25519.pub when it is there, and
// a path with ~/ is in your home folder.
func TestAskKeyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := askKeyFile(typed(""), io.Discard); err == nil {
		t.Error("Enter without a key: want an error")
	}
	if got, err := askKeyFile(typed("~/anna.pub"), io.Discard); err != nil || got != filepath.Join(home, "anna.pub") {
		t.Errorf("~/anna.pub = %q, %v", got, err)
	}
	key := filepath.Join(home, ".ssh", "id_ed25519.pub")
	if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("ssh-ed25519 AAAA ada@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := askKeyFile(typed(""), io.Discard); err != nil || got != key {
		t.Errorf("Enter = %q, %v, want %q", got, err, key)
	}
}

// TestWatcherConnectReadsTheToken: a token left out comes from stdin and
// goes to the server on ssh's stdin, never as an argument.
func TestWatcherConnectReadsTheToken(t *testing.T) {
	dir := t.TempDir()
	sent, runs := filepath.Join(dir, "sent"), filepath.Join(dir, "runs")
	fake := "#!/bin/sh\necho \"$@\" >> " + runs + "\nread -r line\nprintf '%s' \"$line\" > " + sent + "\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	args := []string{"my-server", "https://watcher.example.com"}
	if err := watcherConnect(context.Background(), args, strings.NewReader("secret-token\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	var got daemon.Watcher
	data, _ := os.ReadFile(sent)
	if err := json.Unmarshal(data, &got); err != nil || got.URL != args[1] || got.Token != "secret-token" {
		t.Errorf("the server got %q: %v", data, err)
	}
	if ran, _ := os.ReadFile(runs); strings.Contains(string(ran), "secret-token") {
		t.Errorf("ssh ran with %q", ran)
	}
}
