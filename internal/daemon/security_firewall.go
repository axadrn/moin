package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/axadrn/moin/internal/build"
)

// The firewall is one nftables table of moin's, inet moin, with two
// chains. input guards the server itself. forward guards the ports apps
// publish, which Docker's rules let in from everywhere: they pass through
// forward, never through input.

const (
	tailscalePort = "41641"
	// tailnetInterface is the network card Tailscale makes. What comes in
	// on it comes from the tailnet.
	tailnetInterface = "tailscale0"
	// newConnection matches the first packet of a TCP connection: a knock
	// counts connection attempts, not packets.
	newConnection = "tcp flags & (syn | ack) == syn"
)

// The rules live in a file too, and a small unit loads it at boot.
var (
	firewallFile     = "/etc/" + build.Name + "/firewall.nft"
	firewallUnitFile = "/etc/systemd/system/" + build.Name + "-firewall.service"
)

// firewallUnit is the boot unit that loads the rules with nft at path.
func firewallUnit(nft string) string {
	return `[Unit]
Description=` + build.Name + ` firewall, before the network comes up
DefaultDependencies=no
Before=network-pre.target
Wants=network-pre.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=` + nft + ` -f ` + firewallFile + `

[Install]
WantedBy=sysinit.target
`
}

// openPorts are the ports the firewall lets in from everywhere. web
// opens 80 and 443 (security.web).
func openPorts(file serverFile, web bool) []string {
	ports := []string{}
	if !file.SSHOnTailscale {
		ports = append(ports, "22/tcp")
	}
	if web {
		ports = append(ports, "80/tcp", "443/tcp")
	}
	// Tailscale's direct connections. Closed, it still works, through
	// its relays.
	return append(ports, tailscalePort+"/udp")
}

// firewallRules is moin's nftables table for file, "" when nothing is
// closed. web opens 80 and 443 (security.web). Each connection attempt it
// drops on a well-known port counts under that port's name (knockPorts).
func firewallRules(file serverFile, web bool) string {
	if !file.SSHOnTailscale && !file.Firewall {
		return ""
	}
	var rules strings.Builder
	fmt.Fprintf(&rules, "table inet %s {\n", firewallTable)
	for _, knock := range knockNames() {
		fmt.Fprintf(&rules, "\tcounter %s {\n\t}\n", knockCounter(knock))
	}
	rule := func(text string) { rules.WriteString("\t\t" + text + "\n") }
	rules.WriteString("\tchain input {\n")
	if !file.Firewall {
		// Only SSH closes. Everything else comes in as before.
		rule("type filter hook input priority filter; policy accept;")
		rule("ct state established,related accept")
		rule("iif lo accept")
		rule(fmt.Sprintf("iifname != %q tcp dport 22 %s counter name %s drop", tailnetInterface, newConnection, knockCounter("ssh")))
		rule(fmt.Sprintf("iifname != %q tcp dport 22 drop", tailnetInterface))
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
		// Your tailnet is yours: SSH on any port, and what else you run
		// for it.
		rule(fmt.Sprintf("iifname %q accept", tailnetInterface))
	}
	open := openPorts(file, web)
	for _, port := range open {
		number, protocol, _ := strings.Cut(port, "/")
		rule(fmt.Sprintf("%s dport %s accept", protocol, number))
	}
	for _, knock := range knockPorts {
		closed := slices.DeleteFunc(slices.Clone(knock.Ports), func(port int) bool { return slices.Contains(open, fmt.Sprintf("%d/tcp", port)) })
		if len(closed) > 0 {
			rule(fmt.Sprintf("tcp dport %s %s counter name %s drop", portSet(closed), newConnection, knockCounter(knock.Name)))
		}
	}
	rule(fmt.Sprintf("%s counter name %s drop", newConnection, knockCounter(knockOther)))
	rules.WriteString("\t}\n")
	// A port an app publishes reaches it through Docker's forward rules.
	// This chain comes first and lets in only what comes from the
	// tailnet or from the containers themselves, and the web.
	rules.WriteString("\tchain forward {\n")
	rule("type filter hook forward priority filter - 1; policy accept;")
	rule(fmt.Sprintf("ct status dnat iifname %q accept", tailnetInterface))
	rule(`ct status dnat iifname "docker0" accept`)
	rule(`ct status dnat iifname "br-*" accept`)
	if web {
		rule("ct status dnat ct original proto-dst { 80, 443 } accept")
	}
	rule("ct status dnat drop")
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
// that is loaded already with the same rules stays: a reload would lose
// the knocks counted since the last read.
func setFirewall(ctx context.Context, rules string) error {
	if rules == "" {
		return removeFirewall(ctx)
	}
	nft, err := exec.LookPath("nft")
	if err != nil {
		return errNoNft
	}
	script := "table inet " + firewallTable + "\ndelete table inet " + firewallTable + "\n" + rules
	if body, _ := os.ReadFile(firewallFile); string(body) == script && tableLoaded(ctx) {
		return nil
	}
	command := exec.CommandContext(ctx, nft, "-f", "-")
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("firewall: %s", strings.TrimSpace(string(output)))
	}
	if err := writeWhole(firewallFile, []byte(script)); err != nil {
		return err
	}
	return installFirewallUnit(ctx, nft)
}

// installFirewallUnit writes the boot unit once and enables it.
func installFirewallUnit(ctx context.Context, nft string) error {
	unit := firewallUnit(nft)
	if body, _ := os.ReadFile(firewallUnitFile); string(body) == unit {
		return nil
	}
	if err := writeWhole(firewallUnitFile, []byte(unit)); err != nil {
		return err
	}
	if err := os.Chmod(firewallUnitFile, 0o644); err != nil {
		return err
	}
	name := filepath.Base(firewallUnitFile)
	for _, args := range [][]string{{"daemon-reload"}, {"enable", name}} {
		if output, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
		}
	}
	return nil
}

// removeFirewall takes moin's table away, and the boot unit and the
// rules file with it.
func removeFirewall(ctx context.Context) error {
	if _, err := os.Stat(firewallUnitFile); err == nil {
		_ = exec.CommandContext(ctx, "systemctl", "disable", filepath.Base(firewallUnitFile)).Run()
		_ = os.Remove(firewallUnitFile)
		_ = exec.CommandContext(ctx, "systemctl", "daemon-reload").Run()
	}
	_ = os.Remove(firewallFile)
	if exec.CommandContext(ctx, "nft", "list", "table", "inet", firewallTable).Run() != nil {
		return nil
	}
	if output, err := exec.CommandContext(ctx, "nft", "delete", "table", "inet", firewallTable).CombinedOutput(); err != nil {
		return fmt.Errorf("firewall: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// tableLoaded says whether moin's rules are loaded now: its input chain,
// not just an empty table.
func tableLoaded(ctx context.Context) bool {
	return exec.CommandContext(ctx, "nft", "list", "chain", "inet", firewallTable, "input").Run() == nil
}

// publishedPort is one port in what `docker ps` says, like
// 0.0.0.0:5432->5432/tcp or [::1]:5432->5432/tcp: the host's address and
// port.
var publishedPort = regexp.MustCompile(`(\S+):(\d+)->\d+/(tcp|udp)`)

// publishedPorts are the ports the containers publish on the server, like
// 5432/tcp.
func publishedPorts(ctx context.Context) []string {
	out, err := exec.CommandContext(ctx, "docker", "ps", "--format", "{{.Ports}}").Output()
	if err != nil {
		return []string{}
	}
	return outsidePorts(string(out))
}

// outsidePorts are the ports in what `docker ps` says that others can
// reach: on every address or a public one. A port on loopback, like
// 127.0.0.1:8090, only the server itself reaches.
func outsidePorts(dockerPorts string) []string {
	ports := []string{}
	for _, match := range publishedPort.FindAllStringSubmatch(dockerPorts, -1) {
		ip, err := netip.ParseAddr(strings.Trim(match[1], "[]"))
		if err == nil && ip.IsLoopback() {
			continue
		}
		port := match[2] + "/" + match[3]
		if !slices.Contains(ports, port) {
			ports = append(ports, port)
		}
	}
	slices.Sort(ports)
	return ports
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
