package handler

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
	"github.com/axadrn/moin/internal/build"
)

// ServersHandler is the servers page of the web UI.
type ServersHandler struct {
	servers *service.ServerService
	users   *service.UserService
}

func NewServersHandler(servers *service.ServerService, users *service.UserService) *ServersHandler {
	return &ServersHandler{servers: servers, users: users}
}

func (h *ServersHandler) ServersPage(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	servers, err := h.servers.List(workspace.ID)
	if err != nil {
		renderError(w, "list servers failed", err)
		return
	}
	autoUpdate, err := h.servers.AutoUpdateAgents(workspace.ID)
	if err != nil {
		renderError(w, "read agent updates failed", err)
		return
	}
	render(w, r, pages.Servers(servers, h.servers.CanAdd(workspace.ID), h.removed(r, servers), autoUpdate))
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
	answerDone(w, r, server.Name+" added", "Run the install command on the server.")
}

// NewInstall answers a new install command for a server in the same panel
// as after Add server. The old command stops working.
func (h *ServersHandler) NewInstall(w http.ResponseWriter, r *http.Request) {
	server, installToken, err := h.servers.NewInstallToken(actor(r), r.PathValue("id"))
	if err != nil {
		failed(w, r, "No new install command", err, nil)
		return
	}
	render(w, r, pages.ServerAdded(pages.AddedServer{Name: server.Name, Command: h.servers.InstallCommand(installToken)}))
	answerDone(w, r, "New install command for "+server.Name, "Run it on the server within the hour. The old one stops working.")
}

// Delete removes a server from its page and goes back to the list.
func (h *ServersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	location := ctxkeys.AppBasePath(r.Context()) + "/servers"
	name := ""
	if server, err := h.servers.Get(ctxkeys.Workspace(r.Context()).ID, r.PathValue("id")); err == nil {
		name = server.Name
	}
	uninstall, apps, err := h.servers.Delete(actor(r), r.PathValue("id"))
	if errors.Is(err, repository.ErrServerHasApps) {
		answerFailed(w, r, "Server not removed", "It is online: remove its apps first, so nothing keeps running unseen.")
		return
	}
	if err != nil && !errors.Is(err, repository.ErrServerNotFound) {
		failed(w, r, "Server not removed", err, nil)
		return
	}
	// The list names a server whose agent connected, to show how to remove
	// the agent there.
	if uninstall != "" && name != "" {
		location += "?removed=" + url.QueryEscape(name)
	}
	if len(apps) > 0 {
		answerDone(w, r, "Removed "+name+" and its apps", strings.Join(apps, ", ")+". They stay on the server if it comes back; remove them there with "+build.Name+" agent uninstall --apps.")
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
	answerDone(w, r, "Agent updated", server.Name+" runs agent "+server.AgentVersion+" now.")
}

// AgentUpdates turns automatic agent updates on or off and answers the
// switch as stored, so a failed save flips it back.
func (h *ServersHandler) AgentUpdates(w http.ResponseWriter, r *http.Request) {
	err := h.servers.SetAutoUpdateAgents(actor(r), r.FormValue("on") == "1")
	on, getErr := h.servers.AutoUpdateAgents(ctxkeys.Workspace(r.Context()).ID)
	if getErr != nil {
		failed(w, r, "Setting not saved", getErr, nil)
		return
	}
	if err != nil {
		answerError(w, r, "Setting not saved", errorMessage(err))
	}
	render(w, r, pages.AgentUpdatesForm(on))
}
