package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/axadrn/ops/controlplane/internal/middleware"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/protocol"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	agentHelloTimeout = 10 * time.Second
	agentPingInterval = 15 * time.Second
	agentPingTimeout  = 10 * time.Second
)

// AgentHandler is the surface the ops agent dials into. It is not part of
// /api/v1: agents authenticate with their own tokens, never with a user's.
type AgentHandler struct {
	servers   *service.ServerService
	appURL    string
	clientIPs middleware.ClientIPResolver
}

func NewAgentHandler(servers *service.ServerService, appURL string, clientIPs middleware.ClientIPResolver) *AgentHandler {
	return &AgentHandler{servers: servers, appURL: strings.TrimRight(appURL, "/"), clientIPs: clientIPs}
}

// installScript installs the agent as a systemd service. %[1]s is the
// control plane URL. The binary comes from /download, which serves the
// downloads directory.
const installScript = `#!/bin/sh
# Installs the ops agent. Usage: curl -fsSL %[1]s/install.sh | sudo sh -s -- <install token>
set -eu
token="${1:?usage: install.sh <install token>}"
url="%[1]s"
[ "$(id -u)" = 0 ] || { echo "Run as root." >&2; exit 1; }
[ "$(uname -s)" = Linux ] || { echo "The agent runs on Linux." >&2; exit 1; }
case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) echo "Unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac
# The agent runs as a systemd service. Check before the token is used up.
[ -d /run/systemd/system ] || { echo "The agent needs systemd, and this machine does not run it." >&2; exit 1; }
# Apps run as Docker Compose projects.
if ! docker compose version >/dev/null 2>&1; then
	echo "Installing Docker…"
	curl -fsSL https://get.docker.com | sh
fi
curl -fsSL "$url/download/ops-linux-$arch" -o /usr/local/bin/ops.new
chmod 755 /usr/local/bin/ops.new
mv /usr/local/bin/ops.new /usr/local/bin/ops
OPS_STATE_DIR=/var/lib/ops /usr/local/bin/ops agent register --url "$url" --token "$token"
cat > /etc/systemd/system/ops-agent.service <<'UNIT'
[Unit]
Description=ops agent
After=network-online.target docker.service
Wants=network-online.target docker.service

[Service]
ExecStart=/usr/local/bin/ops agent
Environment=OPS_STATE_DIR=/var/lib/ops
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable ops-agent >/dev/null
systemctl restart ops-agent
echo "The ops agent is running. Check it with: systemctl status ops-agent"
`

// cliScript installs the ops command on a Mac or a Linux machine, logged
// in to this control plane. %[1]s is the control plane URL.
const cliScript = `#!/bin/sh
# Installs the ops command. Usage: curl -fsSL %[1]s/cli.sh | sh
set -eu
url="%[1]s"
case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) echo "ops runs on Linux and macOS." >&2; exit 1 ;;
esac
case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) echo "Unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac
dir=/usr/local/bin
if [ ! -w "$dir" ]; then
	dir="$HOME/.local/bin"
	mkdir -p "$dir"
fi
curl -fsSL "$url/download/ops-$os-$arch" -o "$dir/ops.new"
chmod 755 "$dir/ops.new"
mv "$dir/ops.new" "$dir/ops"
echo "Installed $dir/ops."
case ":$PATH:" in
	*":$dir:"*) ;;
	*) echo "Add $dir to your PATH." ;;
esac
echo "Next: ops login --url $url"
`

// CLIScript answers the script that installs the ops command.
func (h *AgentHandler) CLIScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, cliScript, h.appURL)
}

// InstallScript answers the install script for this control plane.
func (h *AgentHandler) InstallScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, installScript, h.appURL)
}

// Register trades the install token in the Authorization header for an agent token.
func (h *AgentHandler) Register(w http.ResponseWriter, r *http.Request) {
	var request protocol.RegisterRequest
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&request)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	server, agentToken, err := h.servers.Register(agentBearer(r), request.Hello)
	if errors.Is(err, service.ErrInstallTokenValid) {
		http.Error(w, "The install token is invalid, used or expired. Remove the server and add it again for a new install line.", http.StatusUnauthorized)
		return
	}
	if err != nil {
		slog.Error("agent register failed", "error", err)
		http.Error(w, "register failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(protocol.RegisterResponse{ServerID: server.ID, AgentToken: agentToken})
}

// Session holds the agent's WebSocket until either side ends it.
func (h *AgentHandler) Session(w http.ResponseWriter, r *http.Request) {
	server, err := h.servers.Authenticate(agentBearer(r))
	if errors.Is(err, service.ErrAgentToken) {
		http.Error(w, "agent token rejected", http.StatusUnauthorized)
		return
	}
	if err != nil {
		slog.Error("agent authenticate failed", "error", err)
		http.Error(w, "session failed", http.StatusInternalServerError)
		return
	}
	// The server's read and write timeouts are for requests, not sessions.
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Time{})
	_ = controller.SetWriteDeadline(time.Time{})
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	// Log results can be long; the default limit is 32 KB.
	conn.SetReadLimit(4 << 20)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	helloCtx, helloCancel := context.WithTimeout(ctx, agentHelloTimeout)
	var message protocol.Message
	err = wsjson.Read(helloCtx, conn, &message)
	helloCancel()
	var hello protocol.Hello
	if err == nil && message.Type == protocol.TypeHello {
		err = message.Decode(&hello)
	}
	if err != nil || message.Type != protocol.TypeHello {
		conn.Close(websocket.StatusPolicyViolation, "the first message must be hello")
		return
	}

	send := func(message protocol.Message) error {
		sendCtx, sendCancel := context.WithTimeout(ctx, agentPingTimeout)
		defer sendCancel()
		return wsjson.Write(sendCtx, conn, message)
	}
	disconnect := h.servers.Connect(server, hello, h.clientIPs.ClientIP(r), cancel, send)
	defer disconnect()
	slog.Info("agent connected", "server_id", server.ID, "agent_version", hello.AgentVersion)

	go func() {
		defer cancel()
		for {
			var incoming protocol.Message
			err := wsjson.Read(ctx, conn, &incoming)
			if err != nil {
				return
			}
			h.servers.Receive(server.ID, incoming)
		}
	}()

	ticker := time.NewTicker(agentPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "session ended")
			slog.Info("agent disconnected", "server_id", server.ID)
			return
		case <-ticker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, agentPingTimeout)
			err := conn.Ping(pingCtx)
			pingCancel()
			if err != nil {
				cancel()
			}
		}
	}
}

func agentBearer(r *http.Request) string {
	scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	if !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
