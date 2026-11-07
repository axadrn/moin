package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWireTypeSpeaksToTheAgent(t *testing.T) {
	for _, test := range []struct {
		messageType MessageType
		version     int
		want        MessageType
	}{
		{TypeDaemonUpdate, 2, "agent.update"},
		{TypeDaemonURL, 1, "agent.url"},
		{TypeDeployStart, 2, TypeDeployStart},
		{TypeDaemonUpdate, Version, "daemon.update"},
	} {
		if got := WireType(test.messageType, test.version); got != test.want {
			t.Errorf("WireType(%s, %d) = %s, want %s", test.messageType, test.version, got, test.want)
		}
	}
}

func TestDecodeHelloOfTheAgent(t *testing.T) {
	for body, want := range map[string]string{
		`{"protocol_version":2,"agent_version":"v1.0.0-beta.25"}`: "v1.0.0-beta.25",
		`{"protocol_version":3,"daemon_version":"v1.0.0"}`:        "v1.0.0",
	} {
		hello, err := DecodeHello(Message{Type: TypeHello, Data: json.RawMessage(body)})
		if err != nil || hello.DaemonVersion != want {
			t.Errorf("DecodeHello(%s) = %+v, %v", body, hello, err)
		}
	}
}

func TestLegacyUpdateCommandRestartsTheAgentUnit(t *testing.T) {
	got := LegacyUpdateCommand("https://moin.run", "moin", "amd64")
	if !strings.HasSuffix(got, "&& systemctl restart moin-agent") || !strings.Contains(got, "/download/moin-linux-amd64") {
		t.Errorf("LegacyUpdateCommand = %s", got)
	}
}
