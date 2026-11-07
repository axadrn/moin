package api

import (
	"encoding/json"
	"fmt"
	"github.com/axadrn/moin/protocol"
	"log/slog"
	"net/http"
	"time"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/service"
)

// serverEventsHeartbeat keeps proxies from closing a quiet stream.
const serverEventsHeartbeat = 25 * time.Second

// ServersHandler lists, adds and removes the servers of the signed-in user.
// The web UI, the CLI and the TUI all use it.
type ServersHandler struct {
	servers *service.ServerService
}

func NewServersHandler(servers *service.ServerService) *ServersHandler {
	return &ServersHandler{servers: servers}
}

// Server is the public JSON shape of a server. It never carries a token.
type Server struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Online       bool       `json:"online"`
	Hostname     string     `json:"hostname"`
	OS           string     `json:"os"`
	Arch         string     `json:"arch"`
	AgentVersion string     `json:"agent_version"`
	LastSeenAt   *time.Time `json:"last_seen_at"`
	// Tunnel carries app traffic through a Cloudflare Tunnel, with no open
	// port. SSHTailscale answers SSH only on the tailnet; Firewall drops
	// every other inbound connection. Security is what the agent set up.
	Tunnel       bool                         `json:"tunnel"`
	SSHTailscale bool                         `json:"ssh_tailscale"`
	Firewall     bool                         `json:"firewall"`
	Security     *protocol.ServerConfigResult `json:"security"`
	// Rules are what the owner of the server allows moin to do there, from
	// /etc/moin/rules on the server. Null without rules.
	Rules     *protocol.Rules `json:"rules"`
	CreatedAt time.Time       `json:"created_at"`
}

// ServerList is the answer of GET /api/v1/servers and every event of GET
// /api/v1/servers/events.
type ServerList struct {
	Items []Server `json:"items"`
}

// CreatedServer is the answer of POST /api/v1/servers. The install token
// works once, within an hour.
type CreatedServer struct {
	Server         Server    `json:"server"`
	InstallToken   string    `json:"install_token"`
	InstallCommand string    `json:"install_command"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// RemovedServer is the answer of DELETE /api/v1/servers/{id}: the command
// that removes the agent from the server too.
type RemovedServer struct {
	UninstallCommand string `json:"uninstall_command"`
	// Apps went with the server: it was offline, so it could not remove
	// them. They stay on the machine if it comes back.
	Apps []string `json:"apps"`
}

type createServerRequest struct {
	Name string `json:"name"`
}

func NewServer(server model.Server) Server {
	return Server{
		ID:           server.ID,
		Name:         server.Name,
		Online:       server.Online,
		Hostname:     server.Hostname,
		OS:           server.OS,
		Arch:         server.Arch,
		AgentVersion: server.AgentVersion,
		LastSeenAt:   utcTime(server.LastSeenAt),
		Tunnel:       server.Tunnel,
		SSHTailscale: server.SSHTailscale,
		Firewall:     server.Firewall,
		Security:     service.SecurityReport(server),
		Rules:        service.ServerRules(server),
		CreatedAt:    server.CreatedAt.UTC(),
	}
}

func (h *ServersHandler) list(workspaceID string) (ServerList, error) {
	servers, err := h.servers.List(workspaceID)
	if err != nil {
		return ServerList{}, err
	}
	items := make([]Server, 0, len(servers))
	for _, server := range servers {
		items = append(items, NewServer(server))
	}
	return ServerList{Items: items}, nil
}

// List returns the servers by name.
func (h *ServersHandler) List(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	list, err := h.list(workspace.ID)
	if err != nil {
		slog.Error("failed to list servers", "error", err, "workspace_id", workspace.ID)
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, list)
}

// Create adds a server and answers 201 with its install command.
func (h *ServersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var request createServerRequest
	if !Decode(w, r, &request) {
		return
	}
	server, installToken, err := h.servers.Create(actor(r), request.Name)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusCreated, CreatedServer{
		Server:         NewServer(*server),
		InstallToken:   installToken,
		InstallCommand: h.servers.InstallCommand(installToken),
		ExpiresAt:      server.InstallExpiresAt.UTC(),
	})
}

// NewInstall answers 201 with a new install command for the server. The
// old one stops working.
func (h *ServersHandler) NewInstall(w http.ResponseWriter, r *http.Request) {
	server, installToken, err := h.servers.NewInstallToken(actor(r), r.PathValue("id"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusCreated, CreatedServer{
		Server:         NewServer(*server),
		InstallToken:   installToken,
		InstallCommand: h.servers.InstallCommand(installToken),
		ExpiresAt:      server.InstallExpiresAt.UTC(),
	})
}

type renameServerRequest struct {
	Name string `json:"name"`
}

// Rename gives a server a new name and answers it.
func (h *ServersHandler) Rename(w http.ResponseWriter, r *http.Request) {
	var request renameServerRequest
	if !Decode(w, r, &request) {
		return
	}
	server, err := h.servers.Rename(actor(r), r.PathValue("id"), request.Name)
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewServer(*server))
}

// Delete removes a server by ID or name and disconnects its agent;
// ?confirm= is its name, typed out.
func (h *ServersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	uninstall, apps, err := h.servers.Delete(actor(r), r.PathValue("id"), r.URL.Query().Get("confirm"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	if apps == nil {
		apps = []string{}
	}
	JSON(w, http.StatusOK, RemovedServer{UninstallCommand: uninstall, Apps: apps})
}

// Update has the server's agent swap its binary for the control plane's and
// answers the server once the new agent is back.
func (h *ServersHandler) Update(w http.ResponseWriter, r *http.Request) {
	server, err := h.servers.UpdateAgent(r.Context(), actor(r), r.PathValue("id"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewServer(*server))
}

// Events streams the server list as server-sent events: once on connect and
// again after every change.
func (h *ServersHandler) Events(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	changes, unsubscribe := h.servers.Subscribe(workspace.ID)
	defer unsubscribe()

	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func() bool {
		list, err := h.list(workspace.ID)
		if err != nil {
			slog.Error("failed to list servers", "error", err, "workspace_id", workspace.ID)
			return false
		}
		body, err := json.Marshal(list)
		if err != nil {
			return false
		}
		_, err = fmt.Fprintf(w, "event: servers\ndata: %s\n\n", body)
		return err == nil && controller.Flush() == nil
	}
	if !send() {
		return
	}
	heartbeat := time.NewTicker(serverEventsHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			if !send() {
				return
			}
		case <-heartbeat.C:
			_, err := fmt.Fprint(w, ": heartbeat\n\n")
			if err != nil || controller.Flush() != nil {
				return
			}
		}
	}
}
