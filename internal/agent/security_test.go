package agent

import (
	"context"
	"os"
	"path/filepath"
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
	// SSH on Tailscale without the firewall still closes port 22 to the
	// internet, and only that.
	ssh := sshOnTailscaleRules()
	for _, want := range []string{"policy accept", "ct state established,related accept", `iifname != "tailscale0" tcp dport 22 drop`} {
		if !strings.Contains(ssh, want) {
			t.Errorf("SSH on Tailscale rules miss %q:\n%s", want, ssh)
		}
	}
	if path := os.Getenv("MOIN_WRITE_RULES"); path != "" {
		os.WriteFile(path+"-ssh.nft", []byte("table inet moin\ndelete table inet moin\n"+ssh), 0o644)
		os.WriteFile(path+"-direct.nft", []byte("table inet moin\ndelete table inet moin\n"+direct), 0o644)
		os.WriteFile(path+"-tunnel.nft", []byte("table inet moin\ndelete table inet moin\n"+tunnel), 0o644)
	}
}

// fakeCommand puts a shell script named name first on PATH.
func fakeCommand(t *testing.T, name, script string) string {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}

// TestTunnelTokenStaysOffTheCommandLine: docker gets the token through its
// environment, so ps never shows it.
func TestTunnelTokenStaysOffTheCommandLine(t *testing.T) {
	bin := fakeCommand(t, "docker", `dir=$(dirname "$0")
case "$1" in
inspect) exit 1 ;;
run) echo "$*" > "$dir/args"; echo "$TUNNEL_TOKEN" > "$dir/env" ;;
esac
`)
	token := "eyJhIjoidGVzdC10dW5uZWwtdG9rZW4ifQ"
	s := &security{apps: &apps{}}
	if err := s.ensureTunnel(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(bin, "args"))
	env, _ := os.ReadFile(filepath.Join(bin, "env"))
	if strings.Contains(string(args), token) || !strings.Contains(string(args), "-e TUNNEL_TOKEN ") {
		t.Fatalf("docker args = %q", args)
	}
	if strings.TrimSpace(string(env)) != token {
		t.Fatalf("docker env TUNNEL_TOKEN = %q, want the token", env)
	}
}

// TestTailscaleKeyStaysOffTheCommandLine: tailscale reads the auth key from
// a file, and the file is gone afterwards.
func TestTailscaleKeyStaysOffTheCommandLine(t *testing.T) {
	bin := fakeCommand(t, "tailscale", `dir=$(dirname "$0")
case "$1" in
ip) [ -f "$dir/up" ] && echo 100.64.0.7 || exit 1 ;;
up) echo "$*" > "$dir/up"; cat "${2#--auth-key=file:}" > "$dir/key" ;;
esac
`)
	key := "tskey-auth-secret"
	ip, err := joinTailnet(context.Background(), key)
	if err != nil || ip != "100.64.0.7" {
		t.Fatalf("joinTailnet = %q, %v", ip, err)
	}
	args, _ := os.ReadFile(filepath.Join(bin, "up"))
	read, _ := os.ReadFile(filepath.Join(bin, "key"))
	if strings.Contains(string(args), key) || string(read) != key {
		t.Fatalf("tailscale args = %q, key file = %q", args, read)
	}
	file := strings.TrimPrefix(strings.Fields(string(args))[1], "--auth-key=file:")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("key file %s is still there: %v", file, err)
	}
}
