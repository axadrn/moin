package testutil

import (
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/axadrn/moin/protocol"
)

// Hello is the hello of a test daemon that speaks this protocol.
var Hello = protocol.Hello{ProtocolVersion: protocol.Version, DaemonVersion: "v1.0.0", Hostname: "server-1", OS: "linux", Arch: "amd64"}

// AnswerOK is the send of a test daemon for ServerService.Connect: it
// answers every request with ok, as a daemon that keeps every env value
// and secret it gets, and env.set with the keys the app has then. receive
// is ServerService.Receive.
func AnswerOK(receive func(serverID string, message protocol.Message), serverID string) func(protocol.Message) error {
	var mu sync.Mutex
	env := map[string]map[string]bool{}
	return func(message protocol.Message) error {
		if message.RequestID == "" {
			return nil
		}
		answer, _ := protocol.NewMessage(protocol.TypeOK, nil)
		var set protocol.EnvSet
		if message.Type == protocol.TypeEnvSet && message.Decode(&set) == nil {
			mu.Lock()
			if env[set.App] == nil {
				env[set.App] = map[string]bool{}
			}
			for key := range set.Set {
				env[set.App][key] = true
			}
			for _, key := range set.Remove {
				delete(env[set.App], key)
			}
			keys := slices.Sorted(maps.Keys(env[set.App]))
			mu.Unlock()
			answer, _ = protocol.NewMessage(protocol.TypeEnvResult, protocol.AppEnv{Keys: keys, ChangedAt: time.Now().UTC()})
		}
		answer.RequestID = message.RequestID
		go receive(serverID, answer)
		return nil
	}
}
