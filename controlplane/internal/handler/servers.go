package handler

import (
	"errors"
	"net/http"
	"net/url"
	"slices"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// ServersHandler is the servers page of the web UI.
type ServersHandler struct {
	servers *service.ServerService
}

func NewServersHandler(servers *service.ServerService) *ServersHandler {
	return &ServersHandler{servers: servers}
}

func (h *ServersHandler) ServersPage(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	servers, err := h.servers.List(workspace.ID)
	if err != nil {
		renderError(w, "list servers failed", err)
		return
	}
	render(w, r, pages.Servers(servers, h.servers.CanAdd(workspace.ID), h.removed(r, servers)))
}

// removed is the server Delete just removed, named in ?removed=, so the
// list can show the command that removes its agent on the machine.
func (h *ServersHandler) removed(r *http.Request, servers []model.Server) *pages.RemovedServer {
	name := r.URL.Query().Get("removed")
	if name == "" || slices.ContainsFunc(servers, func(server model.Server) bool { return server.Name == name }) {
		return nil
	}
	return &pages.RemovedServer{Name: name, Command: h.servers.UninstallCommand()}
}

// List answers the table, for the reload after a change.
func (h *ServersHandler) List(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	servers, err := h.servers.List(workspace.ID)
	if err != nil {
		renderError(w, "list servers failed", err)
		return
	}
	render(w, r, pages.ServerList(servers))
}

// Create adds a server and answers the reset form and its install command.
// The event stream reloads the table.
func (h *ServersHandler) Create(w http.ResponseWriter, r *http.Request) {
	server, installToken, err := h.servers.Create(actor(r), r.FormValue("name"))
	if errors.Is(err, service.ErrSubscriptionRequired) {
		w.Header().Set("HX-Redirect", ctxkeys.AppBasePath(r.Context())+"/servers")
		return
	}
	if err != nil {
		failed(w, r, "Server not added", err, map[string]string{"name": "server-name"})
		return
	}
	render(w, r, pages.ServerAddForm())
	render(w, r, pages.ServerAdded(pages.AddedServer{Name: server.Name, Command: h.servers.InstallCommand(installToken)}))
	toastDone(w, r, server.Name+" added", "Run the install command on the server.")
}

// Delete removes a server from its page and goes back to the list.
func (h *ServersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	// The list names the server, to show how to remove its agent there.
	location := ctxkeys.AppBasePath(r.Context()) + "/servers"
	if server, err := h.servers.Get(ctxkeys.Workspace(r.Context()).ID, r.PathValue("id")); err == nil {
		location += "?removed=" + url.QueryEscape(server.Name)
	}
	err := h.servers.Delete(actor(r), r.PathValue("id"))
	if errors.Is(err, repository.ErrServerHasApps) {
		toastFailed(w, r, "Server not removed", "Remove the apps on this server first.")
		return
	}
	if err != nil && !errors.Is(err, repository.ErrServerNotFound) {
		failed(w, r, "Server not removed", err, nil)
		return
	}
	redirect(w, r, location, http.StatusSeeOther)
}

// UpdateAgent has the server's agent update itself and says how it went. The
// event stream reloads the list with the new version.
func (h *ServersHandler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	server, err := h.servers.UpdateAgent(r.Context(), actor(r), r.PathValue("id"))
	if err != nil {
		failed(w, r, "Agent not updated", err, nil)
		return
	}
	toastDone(w, r, "Agent updated", server.Name+" runs agent "+server.AgentVersion+" now.")
}
