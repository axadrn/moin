package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"

	"github.com/axadrn/moin/internal/build"
)

// daemonUnit is the systemd unit that runs `moin daemon`, the timers.
var daemonUnit = `[Unit]
Description=` + build.Name + ` daemon
After=network-online.target docker.service
Wants=network-online.target docker.service

[Service]
ExecStart=` + installedPath + ` daemon
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
`

// SetUpHere makes this server run the daemon: it writes name, the name
// you call the server, into the config when it has none yet, and installs
// and starts the systemd unit. Running it again changes nothing.
func SetUpHere(ctx context.Context, name string) error {
	if err := nameServer(name); err != nil {
		return err
	}
	if body, _ := os.ReadFile(unitFile); string(body) != daemonUnit {
		if err := writeWhole(unitFile, []byte(daemonUnit)); err != nil {
			return err
		}
		if err := os.Chmod(unitFile, 0o644); err != nil {
			return err
		}
		if out, err := exec.CommandContext(ctx, "systemctl", "daemon-reload").CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %w: %s", err, out)
		}
	}
	out, err := exec.CommandContext(ctx, "systemctl", "enable", "--now", build.Name+"-daemon").CombinedOutput()
	if err != nil {
		return fmt.Errorf("start the %s daemon: %w: %s", build.Name, err, out)
	}
	return nil
}

// serverName is a name a server can have: it names the server's folder
// in the bucket and the server at its watchers.
var serverName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// nameServer writes name into the config of a server that has no name.
// A name it has stays: its backups live under it in the bucket.
func nameServer(name string) error {
	config, err := readConfig()
	if err != nil || config.Name != "" {
		return err
	}
	if !serverName.MatchString(name) {
		return fmt.Errorf("%q cannot name a server: letters, digits, dots and dashes", name)
	}
	config.Name = name
	return SaveConfig(config)
}
