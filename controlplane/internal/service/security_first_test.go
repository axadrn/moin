package service

import (
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// TestProxyOnFirstConnect: a server without security settings gets no
// config when its agent connects, unless the control plane runs on the
// same server as its apps: then its first connect starts the proxy, and a
// later one sends nothing again.
func TestProxyOnFirstConnect(t *testing.T) {
	apps, _, server, agent := newAppService(t)
	configs := func() []protocol.Message {
		agent.mu.Lock()
		defer agent.mu.Unlock()
		var found []protocol.Message
		for _, message := range agent.sent {
			if message.Type == protocol.TypeServerConfig {
				found = append(found, message)
			}
		}
		return found
	}
	apps.configureServerOnConnect(server.ID, owner.WorkspaceID)
	if len(configs()) != 0 {
		t.Fatal("a plain server got a config on connect")
	}

	apps.ProxyOnFirstConnect()
	done := make(chan struct{})
	go func() {
		apps.configureServerOnConnect(server.ID, owner.WorkspaceID)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(configs()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	sent := configs()
	if len(sent) != 1 {
		t.Fatalf("first connect sent %d configs", len(sent))
	}
	answer, _ := protocol.NewMessage(protocol.TypeServerConfigResult, protocol.ServerConfigResult{Proxy: "direct", OpenPorts: []string{}})
	answer.RequestID = sent[0].RequestID
	apps.servers.Receive(server.ID, answer)
	<-done

	apps.configureServerOnConnect(server.ID, owner.WorkspaceID)
	if len(configs()) != 1 {
		t.Errorf("a later connect sent a config again: %d", len(configs()))
	}
}
