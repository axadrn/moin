package protocol

import (
	"strings"
	"testing"
)

// The update by hand goes to the new name; with legacy it also updates an
// agent still installed under the old one, which then moves itself.
func TestUpdateCommand(t *testing.T) {
	plain := UpdateCommand("https://moin.run/", "moin", "arm64", "")
	if want := "curl -fsSL https://moin.run/download/moin-linux-arm64 -o /usr/local/bin/moin.new && chmod 755 /usr/local/bin/moin.new && mv /usr/local/bin/moin.new /usr/local/bin/moin && systemctl restart moin-agent"; plain != want {
		t.Errorf("UpdateCommand = %s", plain)
	}
	legacy := UpdateCommand("https://moin.run", "moin", "amd64", "ops")
	for _, want := range []string{"systemctl cat moin-agent", "systemctl restart moin-agent", "ln -sf /usr/local/bin/moin /usr/local/bin/ops", "systemctl restart ops-agent"} {
		if !strings.Contains(legacy, want) {
			t.Errorf("legacy command misses %q: %s", want, legacy)
		}
	}
}

// The unit runs the new binary with the new state and keeps the old unit
// from running next to it.
func TestAgentUnit(t *testing.T) {
	unit := AgentUnit("moin", "ops")
	for _, want := range []string{"ExecStart=/usr/local/bin/moin agent\n", "Environment=MOIN_STATE_DIR=/var/lib/moin\n", "Conflicts=ops-agent.service\n", "Restart=always\n", "WantedBy=multi-user.target\n"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit misses %q:\n%s", want, unit)
		}
	}
	if strings.Contains(AgentUnit("moin", ""), "Conflicts") {
		t.Error("a fresh unit names an old one")
	}
}
