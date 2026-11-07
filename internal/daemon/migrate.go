package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// Up to v1.0.0-beta.25 the daemon was called the agent: the unit moin-agent
// ran `moin agent` and kept its state in agent.json. An update puts the new
// binary in place and restarts moin-agent, which then runs `moin agent`
// once more: FromAgent installs and starts moin-daemon, then removes and
// stops moin-agent. Remove this once no server runs an agent.
var (
	legacyUnit      = build.Name + "-agent"
	legacyUnitFile  = "/etc/systemd/system/" + legacyUnit + ".service"
	legacyStateFile = "agent.json"
)

// stopWaitAgent is how long the old unit waits for systemd to stop it.
var stopWaitAgent = 30 * time.Second

// systemctl runs systemctl; tests replace it.
var systemctl = func(ctx context.Context, args ...string) error {
	output, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %v: %w: %s", args, err, output)
	}
	return nil
}

// FromAgent is `moin agent`, which only the old unit runs. It moves the
// server to the daemon and waits for systemd to stop it. When the move
// fails, it runs the daemon under the old unit, so the server stays online,
// and the next start tries again.
func FromAgent(ctx context.Context, dir string) error {
	if _, err := os.Stat(legacyUnitFile); err != nil || os.Getenv("INVOCATION_ID") == "" {
		return fmt.Errorf("`%s agent` is now `%s daemon`", build.Name, build.Name)
	}
	err := moveFromAgent(ctx, dir)
	if err == nil {
		select {
		case <-ctx.Done():
		case <-time.After(stopWaitAgent):
		}
		return nil
	}
	slog.Error("moving to the daemon failed; running under the old unit", "unit", legacyUnit, "error", err)
	state, err := LoadState(dir)
	if err != nil {
		return err
	}
	return Run(ctx, dir, state)
}

// moveFromAgent moves the state, installs and starts moin-daemon, and only
// then disables, removes and stops moin-agent. This process never dials the
// hub: once moin-daemon runs, it is the only one.
func moveFromAgent(ctx context.Context, dir string) error {
	slog.Info("moving from the agent to the daemon", "from", legacyUnit, "to", build.Name+"-daemon")
	if err := moveState(dir); err != nil {
		return err
	}
	if err := os.WriteFile(unitFile, []byte(protocol.DaemonUnit(build.Name)), 0o644); err != nil {
		return err
	}
	if err := systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if err := systemctl(ctx, "enable", "--now", build.Name+"-daemon"); err != nil {
		_ = os.Remove(unitFile)
		_ = systemctl(ctx, "daemon-reload")
		return err
	}
	// moin-daemon runs now: whatever fails from here, this process must
	// not run a second daemon. Disabled while systemd still finds its file;
	// a running unit stays loaded after its file is gone, and systemd
	// forgets it once stopped.
	_ = systemctl(ctx, "disable", legacyUnit)
	if err := os.Remove(legacyUnitFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Error("remove the old unit", "file", legacyUnitFile, "error", err)
	}
	_ = systemctl(ctx, "daemon-reload")
	slog.Info("the daemon runs; stopping the agent")
	if err := systemctl(ctx, "--no-block", "stop", legacyUnit); err != nil {
		slog.Error("stop the old unit", "unit", legacyUnit, "error", err)
	}
	return nil
}

// moveState renames agent.json to daemon.json, its agent_token to
// daemon_token. It does nothing without agent.json.
func moveState(dir string) error {
	old := filepath.Join(dir, legacyStateFile)
	body, err := os.ReadFile(old)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	// A later register wrote daemon.json: that one counts.
	if _, err := os.Stat(filepath.Join(dir, stateFile)); err == nil {
		return os.Remove(old)
	}
	var legacy struct {
		URL        string `json:"url"`
		ServerID   string `json:"server_id"`
		AgentToken string `json:"agent_token"`
	}
	if err := json.Unmarshal(body, &legacy); err != nil {
		return fmt.Errorf("read %s: %w", legacyStateFile, err)
	}
	err = saveState(dir, State{URL: legacy.URL, ServerID: legacy.ServerID, DaemonToken: legacy.AgentToken})
	if err != nil {
		return err
	}
	return os.Remove(old)
}
