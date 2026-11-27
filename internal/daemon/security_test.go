package daemon

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFirewallRules: what each switch closes, and what it leaves open.
func TestFirewallRules(t *testing.T) {
	if rules := firewallRules(serverFile{}); rules != "" {
		t.Fatalf("nothing closed, but rules:\n%s", rules)
	}
	cases := []struct {
		name          string
		file          serverFile
		want, unwants []string
	}{
		{
			name: "ssh only",
			file: serverFile{SSHOnTailscale: true},
			want: []string{"policy accept", "ct state established,related accept", "iif lo accept",
				`iifname != "tailscale0" tcp dport 22 counter name "knock-ssh" drop`},
			unwants: []string{"policy drop", "dport 80", "dport 443", `name "knock-other" drop`},
		},
		{
			name: "firewall web direct ssh public",
			file: serverFile{Firewall: true},
			want: []string{"policy drop", "ct state established,related accept", "tcp dport 22 accept",
				"tcp dport 80 accept", "tcp dport 443 accept", "udp dport 41641 accept",
				`tcp dport { 23, 2323 } counter name "knock-telnet" drop`,
				`tcp dport 5432 counter name "knock-postgres" drop`,
				`meta l4proto tcp counter name "knock-other" drop`},
			unwants: []string{"tailscale0", `"knock-ssh" drop`},
		},
		{
			name: "firewall tunnel ssh on tailscale",
			file: serverFile{Firewall: true, SSHOnTailscale: true, TunnelToken: "token"},
			want: []string{"policy drop", `iifname "tailscale0" tcp dport 22 accept`, "udp dport 41641 accept",
				`tcp dport 22 counter name "knock-ssh" drop`},
			unwants: []string{"tcp dport 80 accept", "tcp dport 443 accept", "\t\ttcp dport 22 accept"},
		},
	}
	for _, c := range cases {
		rules := firewallRules(c.file)
		for _, want := range c.want {
			if !strings.Contains(rules, want) {
				t.Errorf("%s: rules miss %q:\n%s", c.name, want, rules)
			}
		}
		for _, unwant := range c.unwants {
			if strings.Contains(rules, unwant) {
				t.Errorf("%s: rules have %q:\n%s", c.name, unwant, rules)
			}
		}
		// A counter for every knock name, so reading them is the same in
		// each mode.
		for _, name := range knockNames() {
			if !strings.Contains(rules, "counter "+knockCounter(name)+" {") {
				t.Errorf("%s: no counter for %s", c.name, name)
			}
		}
		if path := os.Getenv("MOIN_WRITE_RULES"); path != "" {
			os.WriteFile(filepath.Join(path, strings.ReplaceAll(c.name, " ", "-")+".nft"), []byte(rules), 0o644)
		}
	}
}

// TestKnockPortsAreOnePlaceAndNeverTheWeb: no port is in the list twice,
// and 80 and 443 are not knocks.
func TestKnockPortsAreOnePlaceAndNeverTheWeb(t *testing.T) {
	seen := map[int]string{}
	for _, knock := range knockPorts {
		for _, port := range knock.Ports {
			if port == 80 || port == 443 {
				t.Errorf("%s counts the web port %d", knock.Name, port)
			}
			if other, ok := seen[port]; ok {
				t.Errorf("port %d is %s and %s", port, other, knock.Name)
			}
			seen[port] = knock.Name
		}
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
	t.Setenv("MOIN_CONFIG", filepath.Join(t.TempDir(), "config.yml"))
	bin := fakeCommand(t, "tailscale", `dir=$(dirname "$0")
case "$1" in
status) if [ -f "$dir/up" ]; then echo '{"BackendState":"Running","Self":{"DNSName":"prod.tail0000.ts.net.","TailscaleIPs":["100.64.0.7","fd7a::7"]}}'; else echo '{"BackendState":"NeedsLogin"}'; fi ;;
up) echo "$*" > "$dir/up"; for arg; do case "$arg" in --auth-key=file:*) cat "${arg#--auth-key=file:}" > "$dir/key" ;; esac; done ;;
esac
`)
	key := "tskey-auth-secret"
	tailnet, err := JoinTailnetHere(context.Background(), key, new(strings.Builder))
	if err != nil || tailnet != (Tailnet{IP: "100.64.0.7", Name: "prod.tail0000.ts.net"}) {
		t.Fatalf("JoinTailnetHere = %+v, %v", tailnet, err)
	}
	args, _ := os.ReadFile(filepath.Join(bin, "up"))
	read, _ := os.ReadFile(filepath.Join(bin, "key"))
	if strings.Contains(string(args), key) || string(read) != key {
		t.Fatalf("tailscale args = %q, key file = %q", args, read)
	}
	for _, arg := range strings.Fields(string(args)) {
		if file, ok := strings.CutPrefix(arg, "--auth-key=file:"); ok {
			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Fatalf("key file %s is still there: %v", file, err)
			}
		}
	}
}

// Without nft the firewall says what to install, not an empty error.
func TestFirewallWithoutNftSaysWhatToInstall(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := setFirewall(t.Context(), firewallRules(serverFile{Firewall: true}))
	if !errors.Is(err, errNoNft) {
		t.Fatalf("err = %v, want errNoNft", err)
	}
}

// SSH never closes on a server that is in no tailnet: you would be
// locked out.
func TestSecureNeedsATailnetToCloseSSH(t *testing.T) {
	fakeCommand(t, "tailscale", `echo '{"BackendState":"NeedsLogin"}'`)
	_, err := SecureHere(t.Context(), t.TempDir(), Secure{CloseSSH: true}, new(strings.Builder))
	if err == nil || !strings.Contains(err.Error(), "no tailnet") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidTunnelToken(t *testing.T) {
	good := base64.StdEncoding.EncodeToString([]byte(`{"a":"account","t":"tunnel-id","s":"secret"}`))
	bad := base64.StdEncoding.EncodeToString([]byte(`{"a":"account"}`))
	for token, want := range map[string]bool{good: true, "hello": false, bad: false} {
		if validTunnelToken(token) != want {
			t.Errorf("validTunnelToken(%q) = %v", token, !want)
		}
	}
}
