package daemon

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// TestExecArgs: the command follows the service as its own words, so
// nothing in it reaches a shell, and a word that looks like a flag of
// compose is the command's.
func TestExecArgs(t *testing.T) {
	got := execArgs("shop", "/var/lib/moin/apps/shop/releases/3/compose.json", "web", []string{"sh", "-c", "echo $HOME; rm -rf /"})
	want := []string{"compose", "-p", "moin-shop", "-f", "/var/lib/moin/apps/shop/releases/3/compose.json", "exec", "-T", "web", "sh", "-c", "echo $HOME; rm -rf /"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("execArgs = %q", got)
	}
	got = execArgs("shop", "c.json", "web", []string{"--help"})
	if got[len(got)-2] != "web" || got[len(got)-1] != "--help" {
		t.Errorf("a flag-like command moved: %q", got)
	}
}

// TestExecTimeout: no timeout is the default, and none is more than the most.
func TestExecTimeout(t *testing.T) {
	for _, test := range []struct {
		seconds int
		want    time.Duration
	}{
		{0, 10 * time.Minute},
		{-5, 10 * time.Minute},
		{30, 30 * time.Second},
		{99999, time.Hour},
	} {
		if got := execTimeout(test.seconds); got != test.want {
			t.Errorf("execTimeout(%d) = %v, want %v", test.seconds, got, test.want)
		}
	}
}

// TestCopyCapped: output stops at the cap, says so, and the rest is read
// away so the command never blocks on a full pipe.
func TestCopyCapped(t *testing.T) {
	var got strings.Builder
	reader := strings.NewReader(strings.Repeat("x", 100))
	if copyCapped(reader, 40, func(piece string) { got.WriteString(piece) }) != true {
		t.Error("not truncated")
	}
	if got.Len() != 40 || reader.Len() != 0 {
		t.Errorf("sent %d, left %d", got.Len(), reader.Len())
	}
	got.Reset()
	if copyCapped(strings.NewReader("short"), 40, func(piece string) { got.WriteString(piece) }) || got.String() != "short" {
		t.Errorf("short output: %q", got.String())
	}
}

// TestExecRefused: names that could escape compose, an empty command and
// an app without a release never reach docker.
func TestExecRefused(t *testing.T) {
	a := newApps(t.TempDir(), func(protocol.MessageType, string, any) {})
	for _, start := range []protocol.ExecStart{
		{App: "shop", Service: "web; rm -rf /", Argv: []string{"ls"}},
		{App: "../shop", Service: "web", Argv: []string{"ls"}},
		{App: "shop", Service: "web"},
		{App: "shop", Service: "web", Argv: []string{"ls"}},
	} {
		if _, err := a.runExec(t.Context(), start, func(string) {}); err == nil {
			t.Errorf("%+v ran", start)
		}
	}
}

// TestExecRule: exec: off is a rule like logs: off.
func TestExecRule(t *testing.T) {
	rules, err := parseRules("exec: off\n", "/etc/moin/rules")
	if err != nil || !rules.ExecOff || strings.Join(rules.Lines(), ",") != "exec: off" {
		t.Errorf("exec: off = %+v, %v", rules, err)
	}
	if _, err := parseRules("exec: maybe\n", "/etc/moin/rules"); err == nil {
		t.Error("exec: maybe passed")
	}
}
