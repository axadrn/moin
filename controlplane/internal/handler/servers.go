package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/components/toast"
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
	render(w, r, pages.Servers(servers, h.servers.CanAdd(workspace.ID)))
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
	workspace := ctxkeys.Workspace(r.Context())
	name := r.FormValue("name")
	server, installToken, err := h.servers.Create(actor(r), name)
	switch {
	case errors.Is(err, service.ErrServerName):
		render(w, r, pages.ServerAddForm(name, "Name must contain between 1 and 64 characters."))
		return
	case errors.Is(err, service.ErrSubscriptionRequired):
		w.Header().Set("HX-Redirect", ctxkeys.AppBasePath(r.Context())+"/servers")
		return
	case errors.Is(err, repository.ErrDuplicateServer):
		render(w, r, pages.ServerAddForm(name, "A server with this name already exists."))
		return
	case err != nil:
		slog.Error("failed to add server", "error", err, "workspace_id", workspace.ID)
		renderOOB(w, r, toast.Toast(toast.Props{Title: "Server could not be added", Description: "Please try again.", Type: toast.TypeError}), "beforeend:#toast-container")
		return
	}
	render(w, r, pages.ServerAddForm("", ""))
	render(w, r, pages.ServerAdded(pages.AddedServer{Name: server.Name, Command: h.servers.InstallCommand(installToken)}))
}

// Delete removes a server from its page and goes back to the list.
func (h *ServersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	err := h.servers.Delete(actor(r), r.PathValue("id"))
	if err != nil && !errors.Is(err, repository.ErrServerNotFound) {
		slog.Error("failed to remove server", "error", err, "workspace_id", workspace.ID)
		renderOOB(w, r, toast.Toast(toast.Props{Title: "Server could not be removed", Description: "Please try again.", Type: toast.TypeError}), "beforeend:#toast-container")
		return
	}
	redirect(w, r, ctxkeys.AppBasePath(r.Context())+"/servers", http.StatusSeeOther)
}

// UpdateAgent has the server's agent update itself and says how it went. The
// event stream reloads the list with the new version.
func (h *ServersHandler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	server, err := h.servers.UpdateAgent(r.Context(), actor(r), r.PathValue("id"))
	if err != nil {
		renderOOB(w, r, toast.Toast(toast.Props{Title: "The agent was not updated", Description: errorMessage(err), Type: toast.TypeError}), "beforeend:#toast-container")
		return
	}
	renderOOB(w, r, toast.Toast(toast.Props{Title: "Agent updated", Description: server.Name + " runs agent " + server.AgentVersion + " now.", Type: toast.TypeSuccess}), "beforeend:#toast-container")
}
