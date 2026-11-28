package daemon

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFirewallRules: what each switch closes, and what it leaves open.
func TestFirewallRules(t *testing.T) {
	if rules := firewallRules(serverFile{}, true); rules != "" {
		t.Fatalf("nothing closed, but rules:\n%s", rules)
	}
	syn := "tcp flags & (syn | ack) == syn"
	cases := []struct {
		name          string
		file          serverFile
		web           bool
		want, unwants []string
	}{
		{
			name: "ssh only",
			file: serverFile{SSHOnTailscale: true},
			want: []string{"policy accept", "ct state established,related accept", "iif lo accept",
				`iifname != "tailscale0" tcp dport 22 ` + syn + ` counter name knock_ssh drop`,
				`iifname != "tailscale0" tcp dport 22 drop`},
			unwants: []string{"policy drop", "dport 80", "dport 443", `name knock_other drop`, "chain forward"},
		},
		{
			name: "firewall web direct ssh public",
			file: serverFile{Firewall: true},
			web:  true,
			want: []string{"policy drop", "ct state established,related accept", "tcp dport 22 accept",
				"tcp dport 80 accept", "tcp dport 443 accept", "udp dport 41641 accept",
				`tcp dport { 23, 2323 } ` + syn + ` counter name knock_telnet drop`,
				`tcp dport 5432 ` + syn + ` counter name knock_postgres drop`,
				syn + ` counter name knock_other drop`,
				// A port an app publishes: only the web from everywhere.
				"type filter hook forward priority filter - 1; policy accept;",
				"ct status dnat ct original proto-dst { 80, 443 } accept",
				`ct status dnat iifname "tailscale0" accept`,
				"ct status dnat drop"},
			unwants: []string{`knock_ssh drop`, "\t\tiifname \"tailscale0\" accept"},
		},
		{
			name: "firewall tunnel ssh on tailscale",
			file: serverFile{Firewall: true, SSHOnTailscale: true, TunnelToken: "token"},
			// SSH through the tailnet on any port: sshd may listen on
			// another one.
			want: []string{"policy drop", `iifname "tailscale0" accept`, "udp dport 41641 accept",
				`tcp dport 22 ` + syn + ` counter name knock_ssh drop`, "ct status dnat drop"},
			unwants: []string{"tcp dport 80 accept", "tcp dport 443 accept", "\t\ttcp dport 22 accept", "proto-dst { 80, 443 }"},
		},
	}
	for _, c := range cases {
		rules := firewallRules(c.file, c.web)
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
			if !strings.Contains(rules, "\tcounter "+knockCounter(name)+" {") {
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
	err := setFirewall(t.Context(), firewallRules(serverFile{Firewall: true}, true))
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

// SSH closes on a server in a tailnet, also when no sshd listens on port
// 22: Tailscale SSH may answer it. The CLI's new session over Tailscale
// is the test. Without nft, it stops at the firewall, past the SSH check.
func TestSecureClosesSSHWithoutSSHD(t *testing.T) {
	bin := fakeCommand(t, "tailscale", `echo '{"BackendState":"Running","Self":{"DNSName":"my-server.tail0000.ts.net.","TailscaleIPs":["100.64.0.7"]}}'`)
	t.Setenv("PATH", bin)
	_, err := SecureHere(t.Context(), t.TempDir(), Secure{CloseSSH: true}, new(strings.Builder))
	if !errors.Is(err, errNoNft) {
		t.Fatalf("err = %v, want errNoNft", err)
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

// Tailscale's key: under two weeks left, moin says so. Tailscale that
// does not run is a need too: SSH only comes through it.
func TestTailnetNeeds(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	state, tailnet := parseTailscaleStatus([]byte(`{"BackendState":"Running","Self":{"DNSName":"prod.tail0000.ts.net.","TailscaleIPs":["100.64.0.7"],"KeyExpiry":"2026-12-03T10:00:00Z"}}`))
	if state != "Running" || tailnet.IP != "100.64.0.7" || !tailnet.KeyExpiry.Equal(time.Date(2026, 12, 3, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("parsed %q, %+v", state, tailnet)
	}
	needs := needsOf(state, tailnet, now)
	if len(needs) != 1 || needs[0].key != "tailnet/key" || !strings.Contains(needs[0].title, "December 3") || !strings.Contains(needs[0].text, "Machines, prod,") {
		t.Fatalf("a key that expires in six days: %+v", needs)
	}
	if needs := needsOf(state, tailnet, now.AddDate(0, 0, -30)); len(needs) != 0 {
		t.Fatalf("a key with a month left: %+v", needs)
	}
	// Key expiry turned off: Tailscale names no date.
	_, never := parseTailscaleStatus([]byte(`{"BackendState":"Running","Self":{"DNSName":"prod.tail0000.ts.net.","TailscaleIPs":["100.64.0.7"]}}`))
	if needs := needsOf("Running", never, now); len(needs) != 0 {
		t.Fatalf("no expiry: %+v", needs)
	}
	if needs := needsOf("NeedsLogin", Tailnet{}, now); len(needs) != 1 || needs[0].key != "tailnet/down" {
		t.Fatalf("Tailscale logged out: %+v", needs)
	}
}

// A server that never closed SSH needs nothing from its tailnet.
func TestTailnetNeedsOnlyWithSSHClosed(t *testing.T) {
	fakeCommand(t, "tailscale", `echo '{"BackendState":"Stopped"}'`)
	if needs := tailnetNeeds(t.Context(), t.TempDir(), time.Now()); len(needs) != 0 {
		t.Fatalf("needs = %+v", needs)
	}
}

// The tunnel's label tells tokens apart, but gives away nothing of one.
func TestTokenLabel(t *testing.T) {
	token := base64.StdEncoding.EncodeToString([]byte(`{"a":"account","t":"tunnel-id","s":"secret-secret"}`))
	label := tokenLabel(token)
	if len(label) != 12 || strings.Contains(token, label) || label == tokenLabel(token+"x") || tokenLabel("") != "" {
		t.Fatalf("label %q for %q", label, token)
	}
}

// TestWebNeedsAnApp: a server without apps keeps 80 and 443 closed behind
// the firewall and starts no proxy. Its first deploy opens them, unless a
// tunnel brings the web.
func TestWebNeedsAnApp(t *testing.T) {
	dir := t.TempDir()
	a := newApps(dir)
	s := newSecurity(dir, a)
	s.file = serverFile{Firewall: true, SSHOnTailscale: true}
	// Env set before the first deploy makes the app's folder, no release.
	os.MkdirAll(a.appDir("shop"), 0o700)
	if s.web() || strings.Contains(firewallRules(s.file, s.web()), "dport 443 accept") {
		t.Fatal("a server without apps opens 443")
	}
	os.MkdirAll(a.releaseDir("shop", 1), 0o700)
	if !s.web() || !strings.Contains(firewallRules(s.file, s.web()), "tcp dport 443 accept") {
		t.Fatal("the first deploy does not open 443")
	}
	s.file.TunnelToken = "token"
	if s.web() {
		t.Fatal("behind a tunnel 443 opens")
	}
}
