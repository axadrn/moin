package agent

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

	"github.com/axadrn/ops/protocol"
)

// How a server is reached and locked down. The control plane decides, the
// agent applies it and keeps it in server.json, so the proxy starts in the
// right mode before the first session.
//
//	direct  Traefik takes 80 and 443 and gets certificates itself.
//	tunnel  cloudflared carries all app traffic from Cloudflare to Traefik's
//	        tunnel entrypoint; no port is open. Cloudflare terminates HTTPS.
const (
	serverConfigFile = "server.json"
	tunnelName       = "ops-cloudflared"
	tunnelImage      = "cloudflare/cloudflared:2026.9.3"
	tunnelEntrypoint = "tunnel"
	tunnelPort       = "8080"
	firewallTable    = "ops"
	tailscalePort    = "41641"
)

// security applies the server config.
type security struct {
	dir  string
	apps *apps

	mu     sync.Mutex
	config protocol.ServerConfig
}

func newSecurity(dir string, a *apps) *security {
	s := &security{dir: dir, apps: a}
	body, err := os.ReadFile(filepath.Join(dir, serverConfigFile))
	if err == nil {
		_ = json.Unmarshal(body, &s.config)
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

// apply sets up the proxy, the tunnel, Tailscale and the firewall. A part
// that fails is reported and the rest still applies; SSH only closes when
// Tailscale answers.
func (s *security) apply(ctx context.Context, config protocol.ServerConfig) protocol.ServerConfigResult {
	s.mu.Lock()
	s.config = config
	s.mu.Unlock()
	saveJSON(filepath.Join(s.dir, serverConfigFile), config)
	_ = os.Chmod(filepath.Join(s.dir, serverConfigFile), 0o600)

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

	sshOnTailscale := false
	if config.TailscaleKey != "" {
		result.TailscaleIP, err = joinTailnet(ctx, config.TailscaleKey)
		if err != nil {
			problem("Tailscale: %s", err)
		}
		sshOnTailscale = config.SSHOnTailscale && result.TailscaleIP != ""
		if config.SSHOnTailscale && !sshOnTailscale {
			problem("SSH stays open: Tailscale is not up")
		}
	}

	if config.Firewall {
		ports := openPorts(result.Proxy, sshOnTailscale, config.TailscaleKey != "")
		err = applyFirewall(ctx, firewallRules(ports, sshOnTailscale))
		if err != nil {
			problem("firewall: %s", err)
		} else {
			result.Firewall = true
			result.OpenPorts = ports
		}
	} else {
		_ = removeFirewall(ctx)
	}
	return result
}

// ensureTunnel runs cloudflared with the token, or removes it without one.
func (s *security) ensureTunnel(ctx context.Context, token string) error {
	running, err := s.apps.output(ctx, "docker", "inspect", "-f", "{{.State.Running}} {{.Config.Image}} {{index .Config.Labels \"ops.token\"}}", tunnelName)
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
	_, err = s.apps.output(ctx, "docker", "run", "-d", "--name", tunnelName, "--restart", "unless-stopped",
		"--network", proxyNetwork,
		"--label", "ops.token="+tokenLabel(token),
		"-e", "TUNNEL_TOKEN="+token,
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
	output, err := exec.CommandContext(ctx, "tailscale", "up", "--authkey="+authKey, "--hostname="+hostname).CombinedOutput()
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
func openPorts(proxy string, sshOnTailscale, tailscale bool) []string {
	ports := []string{}
	if !sshOnTailscale {
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

// firewallRules is the nftables table ops owns: inbound only what the
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

// applyFirewall replaces the ops table in one transaction: delete and add
// run together, so the server is never without rules in between.
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
// entry set up, when the agent registers again. The new entry starts with
// both off; the control plane sends its config on connect when it has more.
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
	if err != nil || !slices.Contains(strings.Fields(string(output)), firewallTable) {
		return nil
	}
	return exec.CommandContext(ctx, "nft", "delete", "table", "inet", firewallTable).Run()
}
