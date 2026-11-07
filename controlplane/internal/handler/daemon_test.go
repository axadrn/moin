package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/controlplane/internal/middleware"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/testutil"
	"github.com/axadrn/moin/protocol"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// TestInstallScriptReplacesTheAgent: the install script is valid sh, sets
// up moin-daemon and takes an old moin-agent unit off a server.
func TestInstallScriptReplacesTheAgent(t *testing.T) {
	daemons := NewDaemonHandler(nil, "https://moin.example.com", middleware.NewClientIPResolver(nil), nil)
	recorder := httptest.NewRecorder()
	daemons.InstallScript(recorder, httptest.NewRequest(http.MethodGet, "/install.sh", nil))
	script := recorder.Body.String()
	for _, want := range []string{"systemctl disable --now moin-agent", "rm -f /etc/systemd/system/moin-agent.service", "systemctl restart moin-daemon", "moin daemon register"} {
		if !strings.Contains(script, want) {
			t.Errorf("install.sh misses %q", want)
		}
	}
	if output, err := exec.Command("sh", "-n", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("install.sh is no valid sh: %v %s", err, output)
	}
}

// TestAgentFromBeforeTheDaemonConnectsAndUpdates: a server still running
// the agent (protocol 2) dials the old path with its old token and hello,
// and is told to update in the words it knows.
func TestAgentFromBeforeTheDaemonConnectsAndUpdates(t *testing.T) {
	database := testutil.NewDatabase(t)
	testutil.SeedUser(t, database, testutil.UserOptions{ID: "owner"})
	servers := service.NewServerService(repository.NewServers(database), repository.NewActivity(database), "https://moin.example.com/")
	actor := service.Actor{WorkspaceID: "workspace-owner", UserID: "owner", Source: service.Source{Via: service.ViaWeb}}
	server, _, err := servers.Create(actor, "web-1")
	if err != nil {
		t.Fatal(err)
	}
	token := protocol.LegacyTokenMarker + "old-agent-token"
	hash := sha256.Sum256([]byte(token))
	if _, err := database.Exec(`UPDATE servers SET daemon_token_hash = $1 WHERE id = $2`, hex.EncodeToString(hash[:]), server.ID); err != nil {
		t.Fatal(err)
	}

	daemons := NewDaemonHandler(servers, "https://moin.example.com", middleware.NewClientIPResolver(nil), nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+protocol.LegacySessionPath, daemons.Session)
	hub := httptest.NewServer(mux)
	defer hub.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(hub.URL, "http")+protocol.LegacySessionPath, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	hello := protocol.Message{Type: protocol.TypeHello, Data: json.RawMessage(`{"protocol_version":2,"agent_version":"v1.0.0-beta.25","os":"linux","arch":"arm64"}`)}
	if err := wsjson.Write(ctx, conn, hello); err != nil {
		t.Fatal(err)
	}

	answered := make(chan error, 1)
	go func() {
		for {
			if got, err := servers.Get(actor.WorkspaceID, server.ID); err == nil && got.Online {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		answered <- servers.Request(ctx, server.ID, protocol.TypeDaemonUpdate, protocol.DaemonUpdate{Version: "v9"}, nil)
	}()
	var request protocol.Message
	if err := wsjson.Read(ctx, conn, &request); err != nil {
		t.Fatal(err)
	}
	if request.Type != "agent.update" {
		t.Fatalf("the agent got %q, not the agent.update it knows", request.Type)
	}
	if err := wsjson.Write(ctx, conn, protocol.Message{Type: protocol.TypeOK, RequestID: request.RequestID}); err != nil {
		t.Fatal(err)
	}
	if err := <-answered; err != nil {
		t.Fatal(err)
	}
	got, err := servers.Get(actor.WorkspaceID, server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DaemonVersion != "v1.0.0-beta.25" {
		t.Fatalf("version from agent_version = %q", got.DaemonVersion)
	}
}
