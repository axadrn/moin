package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/testutil"
	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

func TestUpdateDaemon(t *testing.T) {
	database := testutil.NewDatabase(t)
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "owner"})
	activity := repository.NewActivity(database)
	servers := NewServerService(repository.NewServers(database), activity, "https://moin.example.com")
	servers.downloads = t.TempDir()
	server, installToken, err := servers.Create(owner, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := servers.Register(installToken, testHello); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := servers.UpdateDaemon(ctx, owner, "web-1"); !errors.Is(err, ErrServerOffline) {
		t.Fatalf("offline: err = %v", err)
	}

	// The daemon answers like the given one: an old daemon does not know the
	// message, a new one swaps its binary and comes back with the version.
	answer := func(reply protocol.Message, restarted bool) func(protocol.Message) error {
		return func(message protocol.Message) error {
			if message.Type != protocol.TypeDaemonUpdate {
				return nil
			}
			var update protocol.DaemonUpdate
			_ = message.Decode(&update)
			go func() {
				reply.RequestID = message.RequestID
				servers.Receive(server.ID, reply)
				if restarted {
					hello := testHello
					hello.DaemonVersion = update.Version + "+" + update.SHA256[:8]
					servers.Connect(server, hello, "203.0.113.5", func() {}, func(protocol.Message) error { return nil })
				}
			}()
			return nil
		}
	}
	old, _ := protocol.NewMessage(protocol.TypeError, protocol.Error{Message: `this agent does not know "agent.update": update it`})
	servers.Connect(server, testHello, "203.0.113.5", func() {}, answer(old, false))
	if _, err := servers.UpdateDaemon(ctx, owner, "web-1"); !errors.Is(err, ErrNoDaemonBinary) {
		t.Fatalf("no binary: err = %v", err)
	}
	binary := []byte("new daemon")
	if err := os.WriteFile(filepath.Join(servers.downloads, build.Name+"-linux-arm64"), binary, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = servers.UpdateDaemon(ctx, owner, "web-1")
	var daemonErr *DaemonError
	if !errors.As(err, &daemonErr) || !strings.Contains(daemonErr.Message, "too old") || !strings.HasSuffix(daemonErr.Message, "systemctl restart "+build.Name+"-agent") || !strings.Contains(daemonErr.Message, "https://moin.example.com/download/"+build.Name+"-linux-arm64") {
		t.Fatalf("old daemon: err = %v", err)
	}

	ok, _ := protocol.NewMessage(protocol.TypeOK, nil)
	servers.Connect(server, testHello, "203.0.113.5", func() {}, answer(ok, true))
	updated, err := servers.UpdateDaemon(ctx, owner, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(binary)
	if want := build.Version + "+" + hex.EncodeToString(sum[:])[:8]; updated.DaemonVersion != want || !updated.Online {
		t.Fatalf("updated = %s online %v, want %s", updated.DaemonVersion, updated.Online, want)
	}
	// The same version again changes nothing: the activity keeps one row.
	current := testHello
	current.DaemonVersion = updated.DaemonVersion
	servers.Connect(server, current, "203.0.113.5", func() {}, answer(ok, true))
	if _, err := servers.UpdateDaemon(ctx, owner, "web-1"); err != nil {
		t.Fatal(err)
	}
	events, err := activity.ListByWorkspace(owner.WorkspaceID, 50)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		if event.Action == "server.daemon_updated" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("daemon_updated rows = %d, want 1", count)
	}
}
