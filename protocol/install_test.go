package protocol

import (
	"strings"
	"testing"
)

func TestUpdateCommand(t *testing.T) {
	got := UpdateCommand("https://moin.run/", "moin", "arm64")
	if want := "curl -fsSL https://moin.run/download/moin-linux-arm64 -o /usr/local/bin/moin.new && chmod 755 /usr/local/bin/moin.new && mv /usr/local/bin/moin.new /usr/local/bin/moin && systemctl restart moin-agent"; got != want {
		t.Errorf("UpdateCommand = %s", got)
	}
}

func TestAgentUnit(t *testing.T) {
	unit := AgentUnit("moin")
	for _, want := range []string{"ExecStart=/usr/local/bin/moin agent\n", "Environment=MOIN_STATE_DIR=/var/lib/moin\n", "Restart=always\n", "WantedBy=multi-user.target\n"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit misses %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "Conflicts") {
		t.Error("the unit names another one")
	}
}
