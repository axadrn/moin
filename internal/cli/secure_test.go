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
