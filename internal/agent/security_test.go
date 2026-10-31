package agent

import (
	"os"
	"strings"
	"testing"
)

func TestFirewallRules(t *testing.T) {
	direct := firewallRules(openPorts("direct", false, false), false)
	for _, want := range []string{"policy drop", "ct state established,related accept", "tcp dport 22 accept", "tcp dport 80 accept", "tcp dport 443 accept"} {
		if !strings.Contains(direct, want) {
			t.Errorf("direct rules miss %q:\n%s", want, direct)
		}
	}
	tunnel := firewallRules(openPorts("tunnel", true, true), true)
	for _, unwanted := range []string{"tcp dport 80", "tcp dport 443", "\t\ttcp dport 22 accept"} {
		if strings.Contains(tunnel, unwanted) {
			t.Errorf("tunnel rules with SSH on Tailscale open %q:\n%s", unwanted, tunnel)
		}
	}
	for _, want := range []string{`iifname "tailscale0" tcp dport 22 accept`, "udp dport 41641 accept"} {
		if !strings.Contains(tunnel, want) {
			t.Errorf("tunnel rules miss %q:\n%s", want, tunnel)
		}
	}
	if path := os.Getenv("OPS_WRITE_RULES"); path != "" {
		os.WriteFile(path+"-direct.nft", []byte("table inet ops\ndelete table inet ops\n"+direct), 0o644)
		os.WriteFile(path+"-tunnel.nft", []byte("table inet ops\ndelete table inet ops\n"+tunnel), 0o644)
	}
}
