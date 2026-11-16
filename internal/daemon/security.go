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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/axadrn/moin/protocol"
)

// How a server is reached and locked down. The hub decides, the
// daemon applies it and keeps it in server.json, so the proxy starts in the
// right mode before the first session.
//
//	direct  Traefik takes 80 and 443 and gets certificates itself.
//	tunnel  cloudflared carries all app traffic from Cloudflare to Traefik's
//	        tunnel entrypoint; no port is open. Cloudflare terminates HTTPS.
const (
	serverConfigFile = "server.json"
	tunnelImage      = "cloudflare/cloudflared:2026.9.3"
	tunnelEntrypoint = "tunnel"
	tunnelPort       = "8080"
	tailscalePort    = "41641"
)

// security applies the server config and keeps it: after each start and
// every protocol.SecurityEvery it puts back what drifted.
type security struct {
	dir  string
	apps *apps
	// applying lets one apply or check run at a time.
	applying sync.Mutex

	mu     sync.Mutex
	config protocol.ServerConfig
	// known is whether the hub ever sent a config: without one there is
	// nothing to keep, only what is to report.
	known bool
	// fixed is what checks put back that the hub has not heard of yet.
	fixed []string
}

func newSecurity(dir string, a *apps) *security {
	s := &security{dir: dir, apps: a}
	body, err := os.ReadFile(filepath.Join(dir, serverConfigFile))
	if err == nil {
		s.known = json.Unmarshal(body, &s.config) == nil
	}
	return s
}

func (s *security) current() protocol.ServerConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

// proxyMode is the mode the proxy runs in now.
func (s *security) proxyMode() string {
	if s.current().TunnelToken != "" {
		return "tunnel"
	}
	return "direct"
}

// serverConfig answers server.config (ServerConfig) unless the rules say
// network: locked.
func (r *runner) serverConfig(ctx context.Context, message protocol.Message, rules *protocol.Rules) {
	var config protocol.ServerConfig
	if err := message.Decode(&config); err != nil {
		r.fail(message, err)
		return
	}
	if rules != nil && rules.NetworkLocked {
		r.fail(message, refused(r.rulesFile, "network: locked"))
		return
	}
	r.send(protocol.TypeServerConfigResult, message.RequestID, r.security.apply(ctx, config))
}

// keepSecurity checks the server's security after each start and every
// protocol.SecurityEvery, puts back what drifted, and tells the hub what
// is. A session that starts gets the last report at once (reportSecurity).
func (r *runner) keepSecurity(ctx context.Context) {
	ticker := time.NewTicker(protocol.SecurityEvery)
	defer ticker.Stop()
	for {
		r.reportSecurity(r.security.keep(ctx))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// reportSecurity sends a report while a session is up; the fixes in it
// then count as told.
func (r *runner) reportSecurity(report protocol.ServerConfigResult) {
	r.mu.Lock()
	connected := r.conn != nil
	r.mu.Unlock()
	if !connected {
		return
	}
	r.send(protocol.TypeServerSecurity, "", report)
	r.security.told(len(report.Fixed))
}

// apply sets up what the hub asks for. SSH that was off starts again when
// it no longer is.
func (s *security) apply(ctx context.Context, config protocol.ServerConfig) protocol.ServerConfigResult {
	s.applying.Lock()
	defer s.applying.Unlock()
	s.mu.Lock()
	before, known := s.config, s.known
	s.config, s.known = config, true
	s.mu.Unlock()
	saveJSON(filepath.Join(s.dir, serverConfigFile), config)
	_ = os.Chmod(filepath.Join(s.dir, serverConfigFile), 0o600)
	var problems []string
	if known && before.WantSSH() == protocol.SSHOff && config.WantSSH() != protocol.SSHOff {
		if err := startSSHD(ctx); err != nil {
			problems = append(problems, "SSH: "+err.Error())
		}
	}
	result := s.set(ctx, config)
	result.Problems = append(problems, result.Problems...)
	return result
}

// keep checks that the server still has what the hub set and puts back
// what drifted: the tunnel, the tailnet, sshd off, the firewall. It never
// starts sshd. Without a config from the hub it only looks.
func (s *security) keep(ctx context.Context) protocol.ServerConfigResult {
	s.applying.Lock()
	defer s.applying.Unlock()
	s.mu.Lock()
	config, known := s.config, s.known
	s.mu.Unlock()
	if !known {
		after := look(ctx, s.apps)
		return protocol.ServerConfigResult{Proxy: s.proxyMode(), TailscaleIP: after.tailnet, SSH: sshNow(after.sshd, false, false), OpenPorts: []string{}}
	}
	before := look(ctx, s.apps)
	result := s.set(ctx, config)
	fixed := drifted(config, before, look(ctx, s.apps))
	s.mu.Lock()
	s.fixed = append(s.fixed, fixed...)
	result.Fixed = slices.Clone(s.fixed)
	s.mu.Unlock()
	return result
}

// told forgets the first n fixes: the hub has them.
func (s *security) told(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fixed = s.fixed[min(n, len(s.fixed)):]
}

// seen is what a look at the server finds.
type seen struct {
	tunnel  bool
	table   string
	sshd    bool
	tailnet string
}

// look sees what is on the server now: cloudflared, the daemon's
// firewall table, sshd on port 22 and the tailnet address.
func look(ctx context.Context, a *apps) seen {
	var now seen
	running, err := a.output(ctx, "docker", "inspect", "-f", "{{.State.Running}}", tunnelName)
	now.tunnel = err == nil && strings.TrimSpace(running) == "true"
	table, err := exec.CommandContext(ctx, "nft", "list", "table", "inet", firewallTable).Output()
	if err == nil {
		now.table = string(table)
	}
	now.sshd = listening(22)
	now.tailnet = tailnetIP(ctx)
	return now
}

// drifted names what a check put back, for the activity.
func drifted(config protocol.ServerConfig, before, after seen) []string {
	var fixed []string
	if !before.tunnel && after.tunnel {
		fixed = append(fixed, "the Cloudflare Tunnel")
	}
	if before.tailnet == "" && after.tailnet != "" {
		fixed = append(fixed, "the tailnet")
	}
	if before.sshd && !after.sshd {
		fixed = append(fixed, "SSH off")
	}
	if before.table != after.table {
		switch {
		case config.Firewall:
			fixed = append(fixed, "the firewall")
		case config.WantSSH() == protocol.SSHTailscale:
			fixed = append(fixed, "SSH only through Tailscale")
		default:
			fixed = append(fixed, "the firewall off")
		}
	}
	return fixed
}

// set sets up the proxy, the tunnel, Tailscale, sshd and the firewall. A
// part that fails is reported and the rest still applies; SSH only closes
// to everyone else when the server is in a tailnet.
func (s *security) set(ctx context.Context, config protocol.ServerConfig) protocol.ServerConfigResult {
	result := protocol.ServerConfigResult{Proxy: s.proxyMode(), OpenPorts: []string{}}
	problem := func(format string, args ...any) {
		result.Problems = append(result.Problems, fmt.Sprintf(format, args...))
	}
	noLog := func(string, ...any) {}

	err := s.apps.ensureProxy(ctx, noLog)
	if err != nil {
		problem("proxy: %s", err)
	}
	err = s.ensureTunnel(ctx, config.TunnelToken)
	if err != nil {
		problem("Cloudflare Tunnel: %s", err)
	}

	// A tailnet joined by hand counts: the key only joins one.
	ssh := config.WantSSH()
	result.TailscaleIP = tailnetIP(ctx)
	if config.TailscaleKey != "" && result.TailscaleIP == "" {
		result.TailscaleIP, err = joinTailnet(ctx, config.TailscaleKey)
		if err != nil {
			problem("Tailscale: %s", err)
		}
	}
	sshOnTailscale := ssh == protocol.SSHTailscale && result.TailscaleIP != ""
	if ssh == protocol.SSHTailscale && !sshOnTailscale {
		problem("SSH stays open to everyone: the server is in no tailnet")
	}
	if ssh == protocol.SSHOff {
		err = stopSSHD(ctx)
		if err != nil {
			problem("SSH: %s", err)
		}
	}
	sshOpen := ssh == protocol.SSHEveryone || (ssh == protocol.SSHTailscale && !sshOnTailscale)

	tailscaleOnly := false
	switch {
	case config.Firewall:
		ports := openPorts(result.Proxy, sshOpen, result.TailscaleIP != "")
		err = applyFirewall(ctx, firewallRules(ports, sshOnTailscale))
		if err != nil {
			problem("firewall: %s", err)
			sshOpen = true
		} else {
			result.Firewall = true
			result.OpenPorts = ports
			tailscaleOnly = sshOnTailscale
		}
	case sshOnTailscale:
		err = applyFirewall(ctx, sshOnTailscaleRules())
		if err != nil {
			problem("SSH stays open to everyone: %s", err)
			sshOpen = true
		} else {
			tailscaleOnly = true
		}
	default:
		_ = removeFirewall(ctx)
		sshOpen = true
	}
	result.SSH = sshNow(listening(22), tailscaleOnly, !sshOpen && !tailscaleOnly)
	return result
}

// sshNow is who reaches SSH: nobody when sshd does not listen or the
// firewall drops port 22, Tailscale when only it passes, else everyone.
func sshNow(sshd, tailscaleOnly, closed bool) string {
	switch {
	case !sshd || closed:
		return protocol.SSHOff
	case tailscaleOnly:
		return protocol.SSHTailscale
	}
	return protocol.SSHEveryone
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

// sshUnits are the names sshd runs under: ssh on Debian and Ubuntu, which
// may start it from a socket, sshd elsewhere.
var sshUnits = []string{"ssh.socket", "ssh.service", "sshd.service", "sshd.socket"}

// stopSSHD stops sshd and keeps it from starting at boot. A unit the
// server does not have is skipped.
func stopSSHD(ctx context.Context) error {
	if !listening(22) {
		return nil
	}
	for _, unit := range sshUnits {
		_ = exec.CommandContext(ctx, "systemctl", "disable", "--now", unit).Run()
	}
	if listening(22) {
		return errors.New("sshd still listens on port 22; stop it on the server")
	}
	return nil
}

// startSSHD starts sshd again and at boot, once SSH is no longer off.
func startSSHD(ctx context.Context) error {
	if listening(22) {
		return nil
	}
	for _, unit := range []string{"ssh.service", "sshd.service"} {
		if exec.CommandContext(ctx, "systemctl", "enable", "--now", unit).Run() == nil {
			return nil
		}
	}
	return errors.New("sshd did not start; start it on the server")
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
	return err
}

// tokenLabel tells tunnel tokens apart without storing them in a label.
func tokenLabel(token string) string {
	if len(token) < 12 {
		return ""
	}
	return token[len(token)-8:]
}

// tailnetIP is the server's address in a tailnet, empty when Tailscale is
// missing or down.
func tailnetIP(ctx context.Context) string {
	if _, err := exec.LookPath("tailscale"); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ip, err := exec.CommandContext(ctx, "tailscale", "ip", "-4").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(ip))
}

// joinTailnet installs Tailscale when it is missing, brings it up with
// the auth key and returns the server's tailnet IP.
func joinTailnet(ctx context.Context, authKey string) (string, error) {
	_, err := exec.LookPath("tailscale")
	if err != nil {
		install := exec.CommandContext(ctx, "sh", "-c", "curl -fsSL https://tailscale.com/install.sh | sh")
		output, err := install.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("install: %s", lastLine(string(output)))
		}
	}
	ip, err := exec.CommandContext(ctx, "tailscale", "ip", "-4").Output()
	if err == nil && strings.TrimSpace(string(ip)) != "" {
		return strings.TrimSpace(string(ip)), nil
	}
	hostname, _ := os.Hostname()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// tailscale reads the key from a file with "file:", so the key never
	// shows on a command line in ps. Only root can read the file.
	keyFile, err := os.CreateTemp("", tempName("tailscale-key-*"))
	if err != nil {
		return "", err
	}
	defer os.Remove(keyFile.Name())
	_, err = keyFile.WriteString(authKey)
	if closeErr := keyFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	output, err := exec.CommandContext(ctx, "tailscale", "up", "--auth-key=file:"+keyFile.Name(), "--hostname="+hostname).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("up: %s", lastLine(string(output)))
	}
	ip, err = exec.CommandContext(ctx, "tailscale", "ip", "-4").Output()
	if err != nil {
		return "", errors.New("no tailnet address after tailscale up")
	}
	return strings.TrimSpace(string(ip)), nil
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return lines[len(lines)-1]
}

// openPorts are the ports the firewall lets in from everywhere.
func openPorts(proxy string, ssh, tailscale bool) []string {
	ports := []string{}
	if ssh {
		ports = append(ports, "22/tcp")
	}
	if proxy == "direct" {
		ports = append(ports, "80/tcp", "443/tcp")
	}
	if tailscale {
		ports = append(ports, tailscalePort+"/udp")
	}
	return ports
}

// firewallRules is the nftables table the daemon owns: inbound only what the
// server needs, answers to its own connections, loopback and ping.
// Containers are not filtered here: Docker publishes their ports on its
// own chains, and in tunnel mode nothing is published.
func firewallRules(ports []string, sshOnTailscale bool) string {
	var rules strings.Builder
	fmt.Fprintf(&rules, "table inet %s {\n", firewallTable)
	rules.WriteString("\tchain input {\n")
	rules.WriteString("\t\ttype filter hook input priority filter; policy drop;\n")
	rules.WriteString("\t\tct state established,related accept\n")
	rules.WriteString("\t\tct state invalid drop\n")
	rules.WriteString("\t\tiif lo accept\n")
	rules.WriteString("\t\tip protocol icmp accept\n")
	rules.WriteString("\t\tip6 nexthdr icmpv6 accept\n")
	if sshOnTailscale {
		rules.WriteString("\t\tiifname \"tailscale0\" tcp dport 22 accept\n")
	}
	for _, port := range ports {
		number, protocol, _ := strings.Cut(port, "/")
		fmt.Fprintf(&rules, "\t\t%s dport %s accept\n", protocol, number)
	}
	rules.WriteString("\t}\n}\n")
	return rules.String()
}

// sshOnTailscaleRules is the daemon's table without the firewall: it only
// keeps SSH off every network but the tailnet and lets the rest in.
func sshOnTailscaleRules() string {
	var rules strings.Builder
	fmt.Fprintf(&rules, "table inet %s {\n", firewallTable)
	rules.WriteString("\tchain input {\n")
	rules.WriteString("\t\ttype filter hook input priority filter; policy accept;\n")
	rules.WriteString("\t\tct state established,related accept\n")
	rules.WriteString("\t\tiif lo accept\n")
	rules.WriteString("\t\tiifname != \"tailscale0\" tcp dport 22 drop\n")
	rules.WriteString("\t}\n}\n")
	return rules.String()
}

// applyFirewall replaces the daemon's table in one transaction: delete and
// add run together, so the server is never without rules in between.
func applyFirewall(ctx context.Context, rules string) error {
	script := "table inet " + firewallTable + "\ndelete table inet " + firewallTable + "\n" + rules
	command := exec.CommandContext(ctx, "nft", "-f", "-")
	command.Stdin = strings.NewReader(script)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(output)))
	}
	return nil
}

// resetNetwork takes down the firewall and the tunnel an earlier server
// entry set up, when the daemon registers again. The new entry starts with
// both off; the hub sends its config on connect when it has more.
func resetNetwork(ctx context.Context, dir string) {
	if err := removeFirewall(ctx); err != nil {
		slog.Warn("remove the firewall of the earlier server entry", "error", err)
	}
	if _, err := exec.LookPath("docker"); err == nil {
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", tunnelName).Run()
	}
	_ = os.Remove(filepath.Join(dir, serverConfigFile))
}

func removeFirewall(ctx context.Context) error {
	output, err := exec.CommandContext(ctx, "nft", "list", "tables").Output()
	if err != nil {
		return nil
	}
	if !slices.Contains(strings.Fields(string(output)), firewallTable) {
		return nil
	}
	return exec.CommandContext(ctx, "nft", "delete", "table", "inet", firewallTable).Run()
}
