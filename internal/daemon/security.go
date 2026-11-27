package daemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
// Only `moin unlock`, typed on the server, opens port 22 again. No port
// ever opens by itself: the daemon puts the rules back when they are
// gone, and a boot unit loads them before the network comes up.
const (
	serverConfigFile = "server.json"
	tunnelImage      = "cloudflare/cloudflared:2026.9.3"
	tunnelEntrypoint = "tunnel"
	tunnelPort       = "8080"
	tailscalePort    = "41641"
	// tailnetInterface is the network card Tailscale makes. SSH that
	// comes in on it comes from the tailnet.
	tailnetInterface = "tailscale0"
)

// The rules live in a file too, and a small unit loads it at boot.
var (
	firewallFile     = "/etc/" + build.Name + "/firewall.nft"
	firewallUnitFile = "/etc/systemd/system/" + build.Name + "-firewall.service"
)

var firewallUnit = `[Unit]
Description=` + build.Name + ` firewall, before the network comes up
DefaultDependencies=no
Before=network-pre.target
Wants=network-pre.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/sbin/nft -f ` + firewallFile + `

[Install]
WantedBy=sysinit.target
`

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
func loadServerFile(dir string) (file serverFile, ok bool) {
	body, err := os.ReadFile(filepath.Join(dir, serverConfigFile))
	if err != nil {
		return serverFile{}, false
	}
	return file, json.Unmarshal(body, &file) == nil
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
}

// securityNow says what is closed now: what server.json asks for and
// whether the rules are really loaded.
func (s *security) now(ctx context.Context) Security {
	loaded := tableLoaded(ctx)
	now := Security{
		PublicSSH: listening(22) && !(s.file.SSHOnTailscale && loaded),
		Firewall:  s.file.Firewall && loaded,
		Tunnel:    s.file.TunnelToken != "",
		OpenPorts: []string{},
	}
	if now.Firewall {
		now.OpenPorts = openPorts(s.file)
	}
	return now
}

// PublicSSHHere says whether port 22 of this server is open to the
// internet, for the red line in `moin`.
func PublicSSHHere(ctx context.Context, dir string) bool {
	return newSecurity(dir, nil).now(ctx).PublicSSH
}

// Secure is what the CLI closes. It only ever closes: CloseSSH shuts
// public port 22, Firewall every other port, and a TunnelToken brings
// the web through a Cloudflare Tunnel.
type Secure struct {
	CloseSSH    bool   `json:"close_ssh,omitempty"`
	Firewall    bool   `json:"firewall,omitempty"`
	TunnelToken string `json:"tunnel_token,omitempty"`
}

// SecureHere closes what change asks for. SSH closes only when the
// server is in a tailnet, so you never lock yourself out.
func SecureHere(ctx context.Context, dir string, change Secure, out io.Writer) (Security, error) {
	if change.CloseSSH && TailnetHere(ctx).IP == "" {
		return Security{}, errors.New("this server is in no tailnet, so public SSH stays open")
	}
	if change.TunnelToken != "" && !validTunnelToken(change.TunnelToken) {
		return Security{}, errors.New("that is not a Cloudflare Tunnel token. Copy it from the tunnel's install command: the long text after --token")
	}
	if err := installNft(ctx, out); err != nil {
		return Security{}, err
	}
	s := newSecurity(dir, newApps(dir))
	s.file.SSHOnTailscale = s.file.SSHOnTailscale || change.CloseSSH
	s.file.Firewall = s.file.Firewall || change.Firewall
	if change.TunnelToken != "" {
		s.file.TunnelToken = change.TunnelToken
	}
	if err := s.save(); err != nil {
		return Security{}, err
	}
	s.apps.proxyMode = s.proxyMode
	err := s.apply(ctx)
	return s.now(ctx), err
}

// UnlockHere opens public port 22 again, and starts sshd if it is off.
// It is the way back in, typed in your provider's web console.
func UnlockHere(ctx context.Context, dir string) error {
	if os.Geteuid() != 0 {
		return errors.New("run it as root: sudo " + build.Name + " unlock")
	}
	s := newSecurity(dir, newApps(dir))
	s.file.SSHOnTailscale = false
	if err := s.save(); err != nil {
		return err
	}
	if err := setFirewall(ctx, firewallRules(s.file)); err != nil {
		return err
	}
	return startSSHD(ctx)
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

// openPorts are the ports the firewall lets in from everywhere.
func openPorts(file serverFile) []string {
	ports := []string{}
	if !file.SSHOnTailscale {
		ports = append(ports, "22/tcp")
	}
	if file.TunnelToken == "" {
		ports = append(ports, "80/tcp", "443/tcp")
	}
	// Tailscale's direct connections. Closed, it still works, through
	// its relays.
	return append(ports, tailscalePort+"/udp")
}

// firewallRules is moin's nftables table for file, "" when nothing is
// closed. Each knock it drops on a well-known port counts under that
// port's name (knockPorts). Containers are not filtered here: the ports
// an app publishes go through Docker's own chains.
func firewallRules(file serverFile) string {
	if !file.SSHOnTailscale && !file.Firewall {
		return ""
	}
	var rules strings.Builder
	fmt.Fprintf(&rules, "table inet %s {\n", firewallTable)
	for _, knock := range knockNames() {
		fmt.Fprintf(&rules, "\tcounter %s {\n\t}\n", knockCounter(knock))
	}
	rules.WriteString("\tchain input {\n")
	rule := func(text string) { rules.WriteString("\t\t" + text + "\n") }
	if !file.Firewall {
		// Only SSH closes. Everything else comes in as before.
		rule("type filter hook input priority filter; policy accept;")
		rule("ct state established,related accept")
		rule("iif lo accept")
		rule(fmt.Sprintf("iifname != %q tcp dport 22 counter name %s drop", tailnetInterface, knockCounter("ssh")))
		rules.WriteString("\t}\n}\n")
		return rules.String()
	}
	rule("type filter hook input priority filter; policy drop;")
	rule("ct state established,related accept")
	rule("ct state invalid drop")
	rule("iif lo accept")
	rule("ip protocol icmp accept")
	rule("ip6 nexthdr icmpv6 accept")
	// The answer of the provider's DHCP server, for the address.
	rule("udp sport 67 udp dport 68 accept")
	if file.SSHOnTailscale {
		rule(fmt.Sprintf("iifname %q tcp dport 22 accept", tailnetInterface))
	}
	for _, port := range openPorts(file) {
		number, protocol, _ := strings.Cut(port, "/")
		rule(fmt.Sprintf("%s dport %s accept", protocol, number))
	}
	open := openPorts(file)
	for _, knock := range knockPorts {
		closed := slices.DeleteFunc(slices.Clone(knock.Ports), func(port int) bool { return slices.Contains(open, fmt.Sprintf("%d/tcp", port)) })
		if len(closed) > 0 {
			rule(fmt.Sprintf("tcp dport %s counter name %s drop", portSet(closed), knockCounter(knock.Name)))
		}
	}
	rule(fmt.Sprintf("meta l4proto tcp counter name %s drop", knockCounter(knockOther)))
	rules.WriteString("\t}\n}\n")
	return rules.String()
}

// portSet writes ports for nftables: 22, or { 23, 2323 }.
func portSet(ports []int) string {
	if len(ports) == 1 {
		return fmt.Sprint(ports[0])
	}
	words := make([]string, len(ports))
	for i, port := range ports {
		words[i] = fmt.Sprint(port)
	}
	return "{ " + strings.Join(words, ", ") + " }"
}

// errNoNft is the firewall's problem on a server without nftables.
var errNoNft = errors.New("nftables is not installed. Install the package nftables")

// installNft installs nftables on a server that lacks it, with apt.
func installNft(ctx context.Context, out io.Writer) error {
	if _, err := exec.LookPath("nft"); err == nil {
		return nil
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		return errNoNft
	}
	fmt.Fprintln(out, "installing nftables, the firewall of Linux")
	command := exec.CommandContext(ctx, "apt-get", "install", "-y", "-q", "nftables")
	command.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("install nftables: %s", lastLine(string(output)))
	}
	return nil
}

// setFirewall loads rules, or takes moin's table away when rules is "".
// The rules go into firewallFile too, which the boot unit loads. A table
// that is loaded already with the same rules stays: a reload would set
// the knock counters back to zero.
func setFirewall(ctx context.Context, rules string) error {
	if rules == "" {
		return removeFirewall(ctx)
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return errNoNft
	}
	script := "table inet " + firewallTable + "\ndelete table inet " + firewallTable + "\n" + rules
	if body, _ := os.ReadFile(firewallFile); string(body) == script && tableLoaded(ctx) {
		return nil
	}
	command := exec.CommandContext(ctx, "nft", "-f", "-")
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("firewall: %s", strings.TrimSpace(string(output)))
	}
	if err := writeWhole(firewallFile, []byte(script)); err != nil {
		return err
	}
	return installFirewallUnit(ctx)
}

// installFirewallUnit writes the boot unit once and enables it.
func installFirewallUnit(ctx context.Context) error {
	if body, _ := os.ReadFile(firewallUnitFile); string(body) == firewallUnit {
		return nil
	}
	if err := writeWhole(firewallUnitFile, []byte(firewallUnit)); err != nil {
		return err
	}
	if err := os.Chmod(firewallUnitFile, 0o644); err != nil {
		return err
	}
	unit := filepath.Base(firewallUnitFile)
	for _, args := range [][]string{{"daemon-reload"}, {"enable", unit}} {
		if output, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
		}
	}
	return nil
}

// removeFirewall takes moin's table away, and the boot unit with it.
func removeFirewall(ctx context.Context) error {
	if _, err := os.Stat(firewallUnitFile); err == nil {
		_ = exec.CommandContext(ctx, "systemctl", "disable", filepath.Base(firewallUnitFile)).Run()
		_ = os.Remove(firewallUnitFile)
		_ = exec.CommandContext(ctx, "systemctl", "daemon-reload").Run()
	}
	_ = os.Remove(firewallFile)
	if !tableLoaded(ctx) {
		return nil
	}
	if output, err := exec.CommandContext(ctx, "nft", "delete", "table", "inet", firewallTable).CombinedOutput(); err != nil {
		return fmt.Errorf("firewall: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// tableLoaded says whether moin's nftables table is loaded now.
func tableLoaded(ctx context.Context) bool {
	return exec.CommandContext(ctx, "nft", "list", "table", "inet", firewallTable).Run() == nil
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

// listening says whether something listens on the TCP port, IPv4 or IPv6,
// from /proc/net.
func listening(port int) bool {
	local := fmt.Sprintf(":%04X", port)
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		body, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(string(body), "\n") {
			fields := strings.Fields(line)
			// 0A is LISTEN.
			if len(fields) > 3 && strings.HasSuffix(fields[1], local) && fields[3] == "0A" {
				return true
			}
		}
	}
	return false
}

// startSSHD starts sshd, now and at boot, when it does not listen.
func startSSHD(ctx context.Context) error {
	if listening(22) {
		return nil
	}
	for _, unit := range []string{"ssh.service", "sshd.service"} {
		if exec.CommandContext(ctx, "systemctl", "enable", "--now", unit).Run() == nil {
			return nil
		}
	}
	return errors.New("sshd did not start: start it with systemctl start ssh")
}

// ensureTunnel runs cloudflared with the token, or removes it without one.
func (s *security) ensureTunnel(ctx context.Context, token string) error {
	running, err := s.apps.output(ctx, "docker", "inspect", "-f", "{{.State.Running}} {{.Config.Image}} {{index .Config.Labels \""+labelToken+"\"}}", tunnelName)
	want := "true " + tunnelImage + " " + tokenLabel(token)
	if err == nil && strings.TrimSpace(running) == want && token != "" {
		return nil
	}
	if err == nil {
		_, _ = s.apps.output(ctx, "docker", "rm", "-f", tunnelName)
	}
	if token == "" {
		return nil
	}
	// "-e TUNNEL_TOKEN" without a value makes docker take it from its own
	// environment, so the token never shows on a command line in ps.
	_, err = s.apps.outputEnv(ctx, []string{"TUNNEL_TOKEN=" + token}, "docker", "run", "-d", "--name", tunnelName, "--restart", "unless-stopped",
		"--network", proxyNetwork,
		"--label", labelToken+"="+tokenLabel(token),
		"-e", "TUNNEL_TOKEN",
		tunnelImage, "tunnel", "--no-autoupdate", "run")
	if err != nil {
		return fmt.Errorf("Cloudflare Tunnel: %w", err)
	}
	return nil
}

// tokenLabel tells tunnel tokens apart without storing them in a label.
func tokenLabel(token string) string {
	if len(token) < 12 {
		return ""
	}
	return token[len(token)-8:]
}

// validTunnelToken says whether token looks like a Cloudflare Tunnel
// token: base64 of JSON that names the tunnel in "t".
func validTunnelToken(token string) bool {
	body, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		body, err = base64.RawStdEncoding.DecodeString(token)
	}
	var fields struct {
		Tunnel string `json:"t"`
	}
	return err == nil && json.Unmarshal(body, &fields) == nil && fields.Tunnel != ""
}

// Tailnet is this server in a tailnet: its address and its name there.
// Both are empty when it is in none.
type Tailnet struct {
	IP   string `json:"ip"`
	Name string `json:"name"`
}

// TailnetHere asks Tailscale where this server is in the tailnet.
func TailnetHere(ctx context.Context) Tailnet {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return Tailnet{}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body, err := exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
	if err != nil {
		return Tailnet{}
	}
	var status struct {
		BackendState string
		Self         struct {
			DNSName      string
			TailscaleIPs []string
		}
	}
	if json.Unmarshal(body, &status) != nil || status.BackendState != "Running" {
		return Tailnet{}
	}
	tailnet := Tailnet{Name: strings.TrimSuffix(status.Self.DNSName, ".")}
	for _, ip := range status.Self.TailscaleIPs {
		if !strings.Contains(ip, ":") {
			tailnet.IP = ip
			break
		}
	}
	return tailnet
}

// JoinTailnetHere installs Tailscale when it is missing and brings it up,
// with authKey when there is one. Without one, Tailscale writes a link to
// log in to out and waits for it.
func JoinTailnetHere(ctx context.Context, authKey string, out io.Writer) (Tailnet, error) {
	if tailnet := TailnetHere(ctx); tailnet.IP != "" {
		return tailnet, nil
	}
	if _, err := exec.LookPath("tailscale"); err != nil {
		fmt.Fprintln(out, "installing Tailscale")
		install := exec.CommandContext(ctx, "sh", "-c", "curl -fsSL https://tailscale.com/install.sh | sh")
		if output, err := install.CombinedOutput(); err != nil {
			return Tailnet{}, fmt.Errorf("install Tailscale: %s", lastLine(string(output)))
		}
	}
	args := []string{"up", "--timeout=5m"}
	if config, err := LoadConfig(); err == nil && config.Name != "" {
		args = append(args, "--hostname="+config.Name)
	}
	if authKey != "" {
		// Tailscale reads the key from a file with "file:", so the key
		// never shows on a command line in ps. Only root reads the file.
		keyFile, err := os.CreateTemp("", tempName("tailscale-key-*"))
		if err != nil {
			return Tailnet{}, err
		}
		defer os.Remove(keyFile.Name())
		_, err = keyFile.WriteString(authKey)
		if err = errors.Join(err, keyFile.Close()); err != nil {
			return Tailnet{}, err
		}
		args = append(args, "--auth-key=file:"+keyFile.Name())
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	var said bytes.Buffer
	up := exec.CommandContext(ctx, "tailscale", args...)
	up.Stdout, up.Stderr = io.MultiWriter(out, &said), io.MultiWriter(out, &said)
	if err := up.Run(); err != nil {
		return Tailnet{}, fmt.Errorf("tailscale up: %s", lastLine(said.String()))
	}
	tailnet := TailnetHere(ctx)
	if tailnet.IP == "" {
		return Tailnet{}, errors.New("Tailscale is up, but the server has no tailnet address")
	}
	return tailnet, nil
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return lines[len(lines)-1]
}
