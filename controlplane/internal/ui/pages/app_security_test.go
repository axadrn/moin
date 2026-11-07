package pages

import "testing"

// A stored key alone joins no tailnet: the agent joins it only for SSH on
// Tailscale, so the page must not say the join failed.
func TestTailnetText(t *testing.T) {
	for _, c := range []struct {
		ip           string
		key, ssh     bool
		text, prefix string
	}{
		{"100.64.0.9", true, true, "100.64.0.9", "The server's IP"},
		{"", true, true, "not up", "The server could not join"},
		{"", true, false, "not used", "Turn on SSH only on Tailscale"},
		{"", false, false, "not joined", "Add a Tailscale auth key"},
	} {
		text, note := tailnetText(c.ip, c.key, c.ssh), tailnetNote(c.ip, c.key, c.ssh)
		if text != c.text || len(note) < len(c.prefix) || note[:len(c.prefix)] != c.prefix {
			t.Errorf("ip %q key %v ssh %v: %q, %q", c.ip, c.key, c.ssh, text, note)
		}
	}
}
