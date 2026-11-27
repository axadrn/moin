package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// How a server is closed to the internet. Two switches in server.json,
// turned on only from your laptop:
//
//	ssh_on_tailscale  Public port 22 is closed. SSH works only through the
//	                  tailnet. The question after the first deploy turns it on.
//	firewall          Every other port is closed too, but 80 and 443 for the
//	                  web, or none of them behind a Cloudflare Tunnel.
//	                  `moin secure` turns it on.
//
// Before a change closes anything, the server sets a guard: in two
// minutes it runs `moin unlock` by itself. The CLI stops the guard over a
// new SSH session through the tailnet. So a change that locks you out
// opens port 22 again, and one that works stays.
//
// Otherwise only `moin unlock`, typed on the server, opens port 22 again.
// The daemon puts the rules back when they are gone, and a boot unit loads
// them before the network comes up.
const (
	serverConfigFile = "server.json"
	// guardUnit runs `moin unlock` when the CLI does not stop it in time.
	guardUnit  = build.Name + "-unlock-guard"
	guardAfter = 2 * time.Minute
)

// serverFile is server.json, 0600: what is closed on this server.
type serverFile struct {
	// SSHOnTailscale: public port 22 is closed.
	SSHOnTailscale bool `json:"ssh_on_tailscale,omitempty"`
	// Firewall: every port is closed but the ones the server needs.
	Firewall bool `json:"firewall,omitempty"`
	// TunnelToken runs cloudflared. The web comes through it while
	// there is one.
	TunnelToken string `json:"tunnel_token,omitempty"`
	// Registries are the registry logins by host, as <username>:<password>.
	Registries map[string]string `json:"registries,omitempty"`
}

// loadServerFile reads server.json from dir. ok is false when there is
// none.
func loadServerFile(dir string) (serverFile, bool) {
	var file serverFile
	body, err := os.ReadFile(filepath.Join(dir, serverConfigFile))
	if err != nil {
		return file, false
	}
	err = json.Unmarshal(body, &file)
	return file, err == nil
}

// security is what is closed on this server, and the apps it closes
// around.
type security struct {
	dir  string
	apps *apps
	file serverFile
}

func newSecurity(dir string, a *apps) *security {
	file, _ := loadServerFile(dir)
	return &security{dir: dir, apps: a, file: file}
}

// save writes server.json whole, 0600.
func (s *security) save() error {
	return writeState(filepath.Join(s.dir, serverConfigFile), s.file)
}

// proxyMode is "tunnel" while a tunnel token is set, else "direct".
func (s *security) proxyMode() string {
	if s.file.TunnelToken != "" {
		return "tunnel"
	}
	return "direct"
}

// registryLogin is the server's login for a registry host, as
// <username>:<password>, "" for none.
func (s *security) registryLogin(host string) string {
	return s.file.Registries[host]
}

// Security is what is closed on this server, for the CLI.
type Security struct {
	// PublicSSH: port 22 is open to the internet.
	PublicSSH bool `json:"public_ssh"`
	Firewall  bool `json:"firewall"`
	Tunnel    bool `json:"tunnel"`
	// OpenPorts are the ports the firewall lets in from everywhere.
	// Without the firewall every port is open.
	OpenPorts []string `json:"open_ports"`
	// Published are the ports apps publish on the server. Behind the
	// firewall the internet does not reach them, but 80 and 443.
	Published []string `json:"published"`
	// Tailnet is where the server is in the tailnet.
	Tailnet Tailnet `json:"tailnet"`
}

// now says what is closed now: what server.json asks for and whether the
// rules are really loaded.
func (s *security) now(ctx context.Context) Security {
	now := Security{
		PublicSSH: publicSSH(ctx, s.file),
		Firewall:  s.file.Firewall && tableLoaded(ctx),
		Tunnel:    s.file.TunnelToken != "",
		OpenPorts: []string{},
		Published: publishedPorts(ctx),
		Tailnet:   TailnetHere(ctx),
	}
	if now.Firewall {
		now.OpenPorts = openPorts(s.file)
	}
	return now
}

// PublicSSHHere says whether port 22 of this server is open to the
// internet, for the red line in `moin`.
func PublicSSHHere(ctx context.Context, dir string) bool {
	file, _ := loadServerFile(dir)
	return publicSSH(ctx, file)
}

// publicSSH says whether port 22 is open to the internet: sshd listens,
// and file does not close it, or its rules are not loaded.
func publicSSH(ctx context.Context, file serverFile) bool {
	return listening(22) && !(file.SSHOnTailscale && tableLoaded(ctx))
}

// Secure is what the CLI changes. CloseSSH shuts public port 22,
// Firewall every other port, a TunnelToken brings the web through a
// Cloudflare Tunnel, and TunnelOff takes the tunnel away again: the web
// is back on ports 80 and 443.
type Secure struct {
	CloseSSH    bool   `json:"close_ssh,omitempty"`
	Firewall    bool   `json:"firewall,omitempty"`
	TunnelToken string `json:"tunnel_token,omitempty"`
	TunnelOff   bool   `json:"tunnel_off,omitempty"`
}

// SecureHere makes change. SSH closes only when the server is in a
// tailnet and sshd answers on port 22. A tunnel only takes the web when
// cloudflared reaches Cloudflare. Before anything closes, the guard is
// set: the CLI stops it with ConfirmHere.
func SecureHere(ctx context.Context, dir string, change Secure, out io.Writer) (Security, error) {
	if change.CloseSSH && TailnetHere(ctx).IP == "" {
		return Security{}, errors.New("this server is in no tailnet, so public SSH stays open")
	}
	if change.CloseSSH && !listening(22) {
		return Security{}, errors.New("sshd does not listen on port 22, and SSH through the tailnet goes there. Public SSH stays open")
	}
	if change.TunnelToken != "" && !validTunnelToken(change.TunnelToken) {
		return Security{}, errors.New("that is not a Cloudflare Tunnel token. Copy it from the tunnel's install command: the long text after --token")
	}
	if err := installNft(ctx, out); err != nil {
		return Security{}, err
	}
	s := newSecurity(dir, newApps(dir))
	if change.TunnelToken != "" {
		fmt.Fprintln(out, "starting the Cloudflare Tunnel")
		if err := s.ensureTunnel(ctx, change.TunnelToken); err != nil {
			return s.now(ctx), err
		}
		if err := waitTunnel(ctx); err != nil {
			// The web stays where it was.
			_ = s.ensureTunnel(ctx, s.file.TunnelToken)
			return s.now(ctx), err
		}
		s.file.TunnelToken = change.TunnelToken
	}
	if change.TunnelOff {
		s.file.TunnelToken = ""
	}
	if err := armGuard(ctx); err != nil {
		return s.now(ctx), fmt.Errorf("set the guard that opens port 22 again: %w. Nothing closed", err)
	}
	s.file.SSHOnTailscale = s.file.SSHOnTailscale || change.CloseSSH
	s.file.Firewall = s.file.Firewall || change.Firewall
	if err := s.save(); err != nil {
		return s.now(ctx), err
	}
	s.apps.proxyMode = s.proxyMode
	err := s.apply(ctx)
	return s.now(ctx), err
}

// armGuard sets the guard: in guardAfter, `moin unlock` runs by itself.
func armGuard(ctx context.Context) error {
	_ = exec.CommandContext(ctx, "systemctl", "stop", guardUnit+".timer").Run()
	_ = exec.CommandContext(ctx, "systemctl", "reset-failed", guardUnit+".service").Run()
	output, err := exec.CommandContext(ctx, "systemd-run", "--quiet", "--unit="+guardUnit,
		"--on-active="+fmt.Sprint(int(guardAfter.Seconds())), "--timer-property=AccuracySec=1s",
		installedPath, "unlock").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", lastLine(string(output)))
	}
	return nil
}

// ConfirmHere stops the guard and says what is closed now. The CLI runs
// it over a new SSH session through the tailnet: what closed lets you in,
// so it may stay. When the guard ran out already, port 22 is open again,
// and the answer says so.
func ConfirmHere(ctx context.Context, dir string) (Security, error) {
	if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", guardUnit+".timer").Run() == nil {
		if output, err := exec.CommandContext(ctx, "systemctl", "stop", guardUnit+".timer").CombinedOutput(); err != nil {
			return Security{}, fmt.Errorf("stop the guard: %s", lastLine(string(output)))
		}
	}
	return newSecurity(dir, nil).now(ctx), nil
}

// UnlockHere opens public port 22 again, and starts sshd if it is off.
// It is the way back in, typed in your provider's web console. Each step
// runs, even when one before failed.
func UnlockHere(ctx context.Context, dir string) error {
	if os.Geteuid() != 0 {
		return errors.New("run it as root: sudo " + installedPath + " unlock")
	}
	s := newSecurity(dir, nil)
	s.file.SSHOnTailscale = false
	saved := s.save()
	if saved != nil {
		saved = fmt.Errorf("write %s: %w. Port 22 opens now, but the daemon closes it again within 5 minutes", serverConfigFile, saved)
	}
	return errors.Join(saved, setFirewall(ctx, firewallRules(s.file)), startSSHD(ctx))
}

// keepSecurity puts back what is closed, now and every securityEvery:
// after a reboot, or when someone removed the rules. A server that never
// closed anything is left alone.
func keepSecurity(ctx context.Context, dir string, a *apps) {
	ticker := time.NewTicker(securityEvery)
	defer ticker.Stop()
	for {
		if _, ok := loadServerFile(dir); ok {
			s := newSecurity(dir, a)
			if err := s.apply(ctx); err != nil {
				slog.Error("security", "problem", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// apply makes the server match server.json: the proxy in its mode, the
// tunnel, and the firewall rules, now and at boot. A part that fails
// does not stop the others.
func (s *security) apply(ctx context.Context) error {
	return errors.Join(
		s.apps.ensureProxy(ctx, func(string, ...any) {}),
		s.ensureTunnel(ctx, s.file.TunnelToken),
		setFirewall(ctx, firewallRules(s.file)),
	)
}

// resetNetwork takes down the firewall and the tunnel, for an uninstall
// with the apps, and forgets what was closed.
func resetNetwork(ctx context.Context, dir string) {
	if err := removeFirewall(ctx); err != nil {
		slog.Error("remove the firewall", "error", err)
	}
	if _, err := exec.LookPath("docker"); err == nil {
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", tunnelName).Run()
	}
	_ = os.Remove(filepath.Join(dir, serverConfigFile))
}

// OpenSSHHere opens port 22 for an uninstall that keeps the apps: moin
// and its unlock go, so nothing may stay closed behind them. The tunnel
// keeps carrying the web.
func OpenSSHHere(ctx context.Context) error {
	return errors.Join(removeFirewall(ctx), startSSHD(ctx))
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return lines[len(lines)-1]
}
