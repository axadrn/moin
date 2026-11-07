package protocol

import "strings"

// Before protocol 3 (moin v1.0.0-beta.25 and older) the daemon was called
// the agent, and it still speaks that way until it updates. The hub answers
// it on its old session path and in its old words, so it connects, works
// and updates itself into the daemon. Remove this once no server runs one.

// LegacySessionPath is SessionPath before protocol 3.
const LegacySessionPath = "/agent/session"

// LegacyTokenMarker starts the daemon tokens handed out before protocol 3;
// they stay valid.
const LegacyTokenMarker = "agt_"

// LegacyUpdateCommand is UpdateCommand for a daemon from before protocol 3,
// which runs as the unit <name>-agent. The new binary, started by that
// unit, moves itself to <name>-daemon.
func LegacyUpdateCommand(baseURL, name, arch string) string {
	return strings.TrimSuffix(UpdateCommand(baseURL, name, arch), "-daemon") + "-agent"
}

// legacyTypes are the message types that had another name before protocol 3.
var legacyTypes = map[MessageType]MessageType{
	TypeDaemonUpdate: "agent.update",
	TypeDaemonURL:    "agent.url",
}

// WireType is the name a peer speaking version knows a message type by.
func WireType(messageType MessageType, version int) MessageType {
	if version < 3 {
		if legacy, ok := legacyTypes[messageType]; ok {
			return legacy
		}
	}
	return messageType
}

// DecodeHello decodes a hello, also one from before protocol 3, which sent
// the daemon's version as agent_version.
func DecodeHello(message Message) (Hello, error) {
	var hello struct {
		Hello
		AgentVersion string `json:"agent_version"`
	}
	err := message.Decode(&hello)
	if hello.DaemonVersion == "" {
		hello.DaemonVersion = hello.AgentVersion
	}
	return hello.Hello, err
}
