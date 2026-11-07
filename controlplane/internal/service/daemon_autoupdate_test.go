package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/testutil"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// TestDaemonsUpdateThemselves: a daemon older than the control plane gets
// its version when it connects, unless the workspace turned it off.
func TestDaemonsUpdateThemselves(t *testing.T) {
	database := testutil.NewDatabase(t)
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "owner"})
	activity := repository.NewActivity(database)
	servers := NewServerService(repository.NewServers(database), activity, "https://moin.example.com")
	servers.downloads = t.TempDir()
	if err := os.WriteFile(filepath.Join(servers.downloads, build.Name+"-linux-arm64"), []byte("new daemon"), 0o755); err != nil {
		t.Fatal(err)
	}
	server, installToken, err := servers.Create(owner, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := servers.Register(installToken, testHello); err != nil {
		t.Fatal(err)
	}
	servers.UpdateDaemonsTo("v9.0.0")

	updates := make(chan struct{}, 4)
	var daemon func(protocol.Message) error
	daemon = func(message protocol.Message) error {
		if message.Type != protocol.TypeDaemonUpdate {
			return nil
		}
		updates <- struct{}{}
		go func() {
			ok, _ := protocol.NewMessage(protocol.TypeOK, nil)
			ok.RequestID = message.RequestID
			servers.Receive(server.ID, ok)
			hello := testHello
			hello.DaemonVersion = "v9.0.0"
			servers.Connect(server, hello, "203.0.113.5", func() {}, daemon)
		}()
		return nil
	}

	// Turned off, a connect changes nothing.
	if err := servers.SetAutoUpdateDaemons(owner, false); err != nil {
		t.Fatal(err)
	}
	servers.Connect(server, testHello, "203.0.113.5", func() {}, daemon)
	select {
	case <-updates:
		t.Fatal("updated although turned off")
	case <-time.After(300 * time.Millisecond):
	}

	// Turned on, the daemon behind updates right away, once.
	if err := servers.SetAutoUpdateDaemons(owner, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon was not updated")
	}
	// The update ends once the new daemon is back and the activity notes it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		events, err := activity.ListByWorkspace(owner.WorkspaceID, 50)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]string{}
		for _, event := range events {
			found[event.Action] = event.Via
		}
		if found["server.daemon_updated"] == ViaSystem && found["workspace.daemon_updates"] != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("activity = %v", found)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if updated, err := servers.Get(owner.WorkspaceID, "web-1"); err != nil || updated.DaemonVersion != "v9.0.0" {
		t.Fatalf("daemon = %+v, %v", updated, err)
	}
	select {
	case <-updates:
		t.Fatal("a daemon of the control plane's version was updated again")
	case <-time.After(300 * time.Millisecond):
	}
}
