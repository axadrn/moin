package testutil

import "github.com/axadrn/moin/protocol"

// Hello is the hello of a test daemon that speaks this protocol.
var Hello = protocol.Hello{ProtocolVersion: protocol.Version, DaemonVersion: "v1.0.0", Hostname: "server-1", OS: "linux", Arch: "amd64"}

// AnswerOK is the send of a test daemon for ServerService.Connect: it
// answers every request with ok, as a daemon that keeps every env value
// and secret it gets. receive is ServerService.Receive.
func AnswerOK(receive func(serverID string, message protocol.Message), serverID string) func(protocol.Message) error {
	return func(message protocol.Message) error {
		if message.RequestID != "" {
			answer, _ := protocol.NewMessage(protocol.TypeOK, nil)
			answer.RequestID = message.RequestID
			go receive(serverID, answer)
		}
		return nil
	}
}
