package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/axadrn/moin/internal/daemon"
)

// The fix for the tailnet's rules names this computer the way the rules
// know it: by its tags, else by its login.
func TestTailnetRulesFix(t *testing.T) {
	tailnet := daemon.Tailnet{Name: "web-1.tail0000.ts.net", IP: "100.64.0.7"}
	cases := map[string]string{
		`{"BackendState":"Running","Self":{"Tags":["tag:laptop"],"UserID":1},"User":{"1":{"LoginName":"ada@example.com"}}}`: `"src": ["tag:laptop"]`,
		`{"BackendState":"Running","Self":{"UserID":1},"User":{"1":{"LoginName":"ada@example.com"}}}`:                       `"src": ["ada@example.com"]`,
		`{"BackendState":"Running","Self":{}}`: `"src": ["autogroup:member"]`,
	}
	for status, want := range cases {
		var self thisComputer
		if err := json.Unmarshal([]byte(status), &self); err != nil {
			t.Fatal(err)
		}
		fix := tailnetRulesFix("web-1", tailnet, self)
		for _, part := range []string{want, `"dst": ["tag:server"], "ip": ["tcp:22"]`, "Machines, web-1,", "secure web-1 again"} {
			if !strings.Contains(fix, part) {
				t.Errorf("%s: the fix misses %q:\n%s", status, part, fix)
			}
		}
	}
}

// --tunnel takes only off.
func TestSecureTunnelTakesOnlyOff(t *testing.T) {
	err := secure(t.Context(), []string{"web-1", "--tunnel", "on"}, strings.NewReader(""), new(strings.Builder))
	if err == nil || !strings.Contains(err.Error(), "--tunnel takes only off") {
		t.Fatalf("err = %v", err)
	}
}

// printSecurity says port 22 is closed only when moin closed it. A server
// whose sshd listens elsewhere has nothing open on 22, but nothing closed.
func TestPrintSecuritySaysClosedOnlyWhenClosed(t *testing.T) {
	change := daemon.Secure{CloseSSH: true}
	var out strings.Builder
	printSecurity(&out, "web-1", "100.64.0.7", true, change, daemon.Security{})
	if strings.Contains(out.String(), "closed") || strings.Contains(out.String(), "Locked out") {
		t.Errorf("sshd elsewhere, yet it says:\n%s", out.String())
	}
	out.Reset()
	printSecurity(&out, "web-1", "100.64.0.7", true, change, daemon.Security{SSHOnTailscale: true})
	for _, part := range []string{"Public port 22 is closed", "Locked out one day?"} {
		if !strings.Contains(out.String(), part) {
			t.Errorf("closed, but it misses %q:\n%s", part, out.String())
		}
	}
}

// What a server answers about its tailnet reaches ssh and the terminal
// only when it is a Tailscale address and a plain name.
func TestCheckTailnet(t *testing.T) {
	cases := []struct {
		tailnet daemon.Tailnet
		name    string
		refused bool
	}{
		{daemon.Tailnet{IP: "100.64.0.7", Name: "web-1.tail0000.ts.net"}, "web-1.tail0000.ts.net", false},
		{daemon.Tailnet{IP: "fd7a:115c:a1e0::7", Name: "web-1"}, "web-1", false},
		{daemon.Tailnet{IP: "100.64.0.7", Name: "x$(id)"}, "", false},
		{daemon.Tailnet{IP: "100.64.0.7", Name: "web-1\x1b[2J"}, "", false},
		{daemon.Tailnet{IP: "100.64.0.7", Name: "-oProxyCommand=id"}, "", false},
		{daemon.Tailnet{IP: "203.0.113.7", Name: "web-1"}, "", true},
		{daemon.Tailnet{IP: "100.64.0.7$(id)", Name: "web-1"}, "", true},
	}
	for _, c := range cases {
		got, err := checkTailnet("web-1", c.tailnet)
		if (err != nil) != c.refused {
			t.Errorf("%+v: err = %v, refused should be %v", c.tailnet, err, c.refused)
			continue
		}
		if err == nil && got.Name != c.name {
			t.Errorf("%+v: name = %q, want %q", c.tailnet, got.Name, c.name)
		}
	}
}
