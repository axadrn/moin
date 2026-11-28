package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/axadrn/moin/internal/daemon"
)

// TestAppArgs: the app comes first and may be left out in its folder.
func TestAppArgs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shop")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, test := range []struct {
		args   []string
		others int
		app    string
		rest   []string
	}{
		{[]string{}, 0, "shop", []string{}},
		{[]string{"blog"}, 0, "blog", []string{}},
		{[]string{"shop.example.com"}, 1, "shop", []string{"shop.example.com"}},
		{[]string{"blog", "blog.example.com"}, 1, "blog", []string{"blog.example.com"}},
	} {
		flags := newFlags("test", "test [app]").withServer()
		if err := flags.parse(test.args); err != nil {
			t.Fatal(err)
		}
		app, rest, err := appArgs(flags, test.others)
		if err != nil || app != test.app || !reflect.DeepEqual(rest, test.rest) {
			t.Errorf("appArgs(%q, %d) = %q, %q, %v; want %q, %q", test.args, test.others, app, rest, err, test.app, test.rest)
		}
	}
	flags := newFlags("test", "test [app]")
	_ = flags.parse([]string{"a", "b", "c"})
	if _, _, err := appArgs(flags, 0); err == nil {
		t.Error("three arguments for one app: want an error")
	}
	flags = newFlags("test", "test [app]")
	_ = flags.parse([]string{"Not_An_App"})
	if _, _, err := appArgs(flags, 0); err == nil {
		t.Error("a bad app name: want an error")
	}
}

// TestServerFlag: -s and --server name the same server, before or after
// the app.
func TestServerFlag(t *testing.T) {
	for _, args := range [][]string{{"shop", "-s", "prod"}, {"--server", "prod", "shop"}} {
		flags := newFlags("logs", "logs [app]").withServer()
		if err := flags.parse(args); err != nil {
			t.Fatal(err)
		}
		if *flags.serverFlag != "prod" || flags.Arg(0) != "shop" {
			t.Errorf("%q: server %q, app %q", args, *flags.serverFlag, flags.Arg(0))
		}
	}
}

// TestRememberApps keeps what each server that answered runs, and what is
// known of a server that did not answer.
func TestRememberApps(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rememberApp("prod", "shop")
	rememberApp("prod", "blog")
	rememberApp("staging", "shop")
	rememberApp("prod", "shop")
	if got := knownServers("shop"); !reflect.DeepEqual(got, []string{"prod", "staging"}) {
		t.Fatalf("shop runs on %q", got)
	}
	rememberApps([]serverRow{
		{server: "prod", status: daemon.Status{Apps: []daemon.AppStatus{{Name: "blog"}, {Name: "wiki"}}}},
		{server: "staging", err: errNoApp},
	})
	apps := loadApps()
	if !reflect.DeepEqual(apps["shop"], []string{"staging"}) {
		t.Errorf("shop runs on %q, want only staging: prod no longer runs it, staging did not answer", apps["shop"])
	}
	if !reflect.DeepEqual(apps["wiki"], []string{"prod"}) || !reflect.DeepEqual(apps["blog"], []string{"prod"}) {
		t.Errorf("apps = %v", apps)
	}
}

// TestServersFile: one name per line, once, comments and blanks skipped.
func TestServersFile(t *testing.T) {
	if got := parseServers("prod\n\n# old\nroot@203.0.113.7\nprod\n"); !slices.Equal(got, []string{"prod", "root@203.0.113.7"}) {
		t.Errorf("parseServers = %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, server := range []string{"prod", "staging", "prod"} {
		if err := addServer(server); err != nil {
			t.Fatal(err)
		}
	}
	got, err := loadServers()
	if err != nil || !slices.Equal(got, []string{"prod", "staging"}) {
		t.Errorf("servers = %q, %v", got, err)
	}
}

// TestShortServerName: my-server means the saved root@my-server, when it
// is the only one with that host. Any other name stays as typed.
func TestShortServerName(t *testing.T) {
	servers := []string{"root@my-server", "my-other", "root@web-1", "deploy@web-1"}
	for name, want := range map[string]string{
		"my-server":      "root@my-server",
		"root@my-server": "root@my-server",
		"my-other":       "my-other",
		"web-1":          "web-1",
		"ada@my-server":  "ada@my-server",
		"new-server":     "new-server",
	} {
		if got := matchServer(servers, name); got != want {
			t.Errorf("matchServer(%q) = %q, want %q", name, got, want)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := addServer("root@my-server"); err != nil {
		t.Fatal(err)
	}
	flags := newFlags("secure", "secure <server>").withServer()
	if err := flags.parse([]string{"my-server", "-s", "my-server"}); err != nil {
		t.Fatal(err)
	}
	server, _, err := flags.serverArgs(strings.NewReader(""), 0)
	if err != nil || server != "root@my-server" || flags.serverNamed() != "root@my-server" {
		t.Errorf("server %q, -s %q, err %v", server, flags.serverNamed(), err)
	}
}

// TestTwoServersNeedsS: moin never guesses between two servers.
func TestTwoServersNeedsS(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rememberApp("prod", "shop")
	rememberApp("staging", "shop")
	flags := newFlags("logs", "logs [app]").withServer()
	ran := ""
	err := onApp(t.Context(), flags, "shop", func(server string) error { ran = server; return nil })
	if err == nil || ran != "" {
		t.Fatalf("ran on %q, err %v: want an error that asks for -s", ran, err)
	}
	*flags.serverFlag = "staging"
	if err := onApp(t.Context(), flags, "shop", func(server string) error { ran = server; return nil }); err != nil || ran != "staging" {
		t.Errorf("with -s staging ran on %q, %v", ran, err)
	}
}

// TestOnAppRunsOnce: after the known server says no, moin asks the others
// and does not run the command there again.
func TestOnAppRunsOnce(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	rememberApp("prod", "shop")
	runs := 0
	err := onApp(t.Context(), newFlags("logs", "logs [app]").withServer(), "shop", func(string) error { runs++; return errNoApp })
	if runs != 1 || err == nil || !strings.Contains(err.Error(), "none of your servers runs shop") {
		t.Errorf("ran %d times, err %v", runs, err)
	}
}
