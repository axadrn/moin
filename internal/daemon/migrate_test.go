package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSystemd puts the unit files into a temp dir and records the
// systemctl calls; fail makes the call starting with it fail.
func fakeSystemd(t *testing.T, fail string) (units string, calls *[]string) {
	t.Helper()
	units = t.TempDir()
	oldUnit, oldLegacy, oldSystemctl, oldWait := unitFile, legacyUnitFile, systemctl, stopWaitAgent
	unitFile = filepath.Join(units, "moin-daemon.service")
	legacyUnitFile = filepath.Join(units, "moin-agent.service")
	stopWaitAgent = 10 * time.Millisecond
	calls = &[]string{}
	systemctl = func(_ context.Context, args ...string) error {
		call := strings.Join(args, " ")
		*calls = append(*calls, call)
		if fail != "" && strings.HasPrefix(call, fail) {
			return errors.New("failed")
		}
		return nil
	}
	t.Cleanup(func() { unitFile, legacyUnitFile, systemctl, stopWaitAgent = oldUnit, oldLegacy, oldSystemctl, oldWait })
	t.Setenv("INVOCATION_ID", "x")
	if err := os.WriteFile(legacyUnitFile, []byte("ExecStart=/usr/local/bin/moin agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return units, calls
}

func writeAgentState(t *testing.T, dir string) {
	t.Helper()
	body := `{"url":"https://moin.example.com","server_id":"s1","agent_token":"agt_secret"}`
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestFromAgentMovesTheServerToTheDaemon: the old unit, running the new
// binary, moves the state, starts moin-daemon and only then removes and
// stops moin-agent.
func TestFromAgentMovesTheServerToTheDaemon(t *testing.T) {
	_, calls := fakeSystemd(t, "")
	dir := t.TempDir()
	writeAgentState(t, dir)

	if err := FromAgent(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(dir)
	if err != nil || state.DaemonToken != "agt_secret" || state.ServerID != "s1" || state.URL != "https://moin.example.com" {
		t.Fatalf("state = %+v, %v", state, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("agent.json is still there")
	}
	unit, err := os.ReadFile(unitFile)
	if err != nil || !strings.Contains(string(unit), "ExecStart=/usr/local/bin/moin daemon\n") {
		t.Fatalf("daemon unit = %q, %v", unit, err)
	}
	if _, err := os.Stat(legacyUnitFile); !errors.Is(err, os.ErrNotExist) {
		t.Error("the agent unit is still there")
	}
	want := []string{"daemon-reload", "enable --now moin-daemon", "disable moin-agent", "daemon-reload", "--no-block stop moin-agent"}
	if strings.Join(*calls, "; ") != strings.Join(want, "; ") {
		t.Fatalf("systemctl calls:\n%s\nwant\n%s", strings.Join(*calls, "; "), strings.Join(want, "; "))
	}
}

// TestFromAgentKeepsTheAgentWhenTheDaemonDoesNotStart: without moin-daemon
// running, moin-agent stays as it was and the next start tries again.
func TestFromAgentKeepsTheAgentWhenTheDaemonDoesNotStart(t *testing.T) {
	_, calls := fakeSystemd(t, "enable")
	dir := t.TempDir()
	if err := moveFromAgent(context.Background(), dir); err == nil {
		t.Fatal("the move did not fail")
	}
	if _, err := os.Stat(legacyUnitFile); err != nil {
		t.Error("the agent unit is gone, with no daemon to take over")
	}
	if _, err := os.Stat(unitFile); !errors.Is(err, os.ErrNotExist) {
		t.Error("the daemon unit that did not start is still there")
	}
	for _, call := range *calls {
		if strings.Contains(call, "moin-agent") {
			t.Errorf("touched the agent: systemctl %s", call)
		}
	}
}

// TestAgentCommandIsGoneOutsideTheOldUnit: `moin agent` by hand only
// points to `moin daemon`.
func TestAgentCommandIsGoneOutsideTheOldUnit(t *testing.T) {
	fakeSystemd(t, "")
	t.Setenv("INVOCATION_ID", "")
	err := FromAgent(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "is now `moin daemon`") {
		t.Fatalf("err = %v", err)
	}
}

// TestLoadStateMovesAgentJSON: a daemon started on a server that still has
// agent.json reads it as daemon.json.
func TestLoadStateMovesAgentJSON(t *testing.T) {
	dir := t.TempDir()
	writeAgentState(t, dir)
	state, err := LoadState(dir)
	if err != nil || state.DaemonToken != "agt_secret" {
		t.Fatalf("state = %+v, %v", state, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "daemon.json")); err != nil {
		t.Fatal(err)
	}
}
