package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/axadrn/ops/internal/build"
)

// Where install.sh puts the agent.
var (
	unitFile      = "/etc/systemd/system/" + build.Name + "-agent.service"
	installedPath = "/usr/local/bin/" + build.Name
)

// Uninstall removes the agent from this server: its systemd service, its
// binary and its state in dir. The apps keep running with their data, the
// proxy, the tunnel and the firewall, unless withApps: then it takes all of
// them down too, data included. It returns what stays on the server.
func Uninstall(ctx context.Context, dir string, withApps bool, logf func(string, ...any)) ([]string, error) {
	_, err := os.Stat(unitFile)
	installed := err == nil
	if installed && os.Geteuid() != 0 {
		return nil, errors.New("run it as root, or put sudo in front: sudo " + build.Name + " agent uninstall")
	}
	if installed {
		logf("stopping the %s-agent service", build.Name)
		_ = exec.CommandContext(ctx, "systemctl", "disable", "--now", build.Name+"-agent").Run()
		err = os.Remove(unitFile)
		if err != nil {
			return nil, err
		}
		_ = exec.CommandContext(ctx, "systemctl", "daemon-reload").Run()
	}

	a := newApps(dir, nil)
	appNames := a.names()
	_, err = exec.LookPath("docker")
	hasDocker := err == nil
	if withApps {
		for _, app := range appNames {
			if !hasDocker {
				break
			}
			logf("removing %s with its data", app)
			_, err := a.output(ctx, "docker", "compose", "-p", project(app), "down", "--volumes", "--rmi", "all", "--remove-orphans")
			if err != nil {
				return nil, fmt.Errorf("remove %s: %w", app, err)
			}
		}
		logf("removing the proxy, the tunnel and the firewall")
		_, _ = a.output(ctx, "docker", "rm", "-f", proxyName)
		_, _ = a.output(ctx, "docker", "network", "rm", proxyNetwork)
		resetNetwork(ctx, dir)
		err = os.RemoveAll(dir)
		if err != nil {
			return nil, err
		}
	} else {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			// The apps keep their data and the proxy its certificates.
			if entry.Name() == "apps" || entry.Name() == "traefik" {
				continue
			}
			err = os.RemoveAll(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
		}
	}

	if executable, err := os.Executable(); err == nil && executable == installedPath {
		logf("removing %s", installedPath)
		err = os.Remove(installedPath)
		if err != nil {
			return nil, err
		}
	}

	var kept []string
	if !withApps && len(appNames) > 0 {
		kept = append(kept, fmt.Sprintf("the apps %s with their data in %s", strings.Join(appNames, ", "), filepath.Join(dir, "apps")))
	}
	if !withApps && hasDocker {
		if _, err := a.output(ctx, "docker", "inspect", proxyName); err == nil {
			kept = append(kept, "the proxy container "+proxyName+", and the tunnel and firewall when they were on")
		}
	}
	if _, err := os.Stat(RulesFile()); err == nil {
		kept = append(kept, "your rules in "+RulesFile())
	}
	if hasDocker {
		kept = append(kept, "Docker")
	}
	return kept, nil
}

// names lists the apps this server has a directory for.
func (a *apps) names() []string {
	entries, _ := os.ReadDir(a.dir)
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && appNamePattern.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names
}
