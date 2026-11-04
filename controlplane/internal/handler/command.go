package handler

import (
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
)

// CommandHandler answers what ⌘K lists past the pages.
type CommandHandler struct {
	apps    *service.AppService
	servers *service.ServerService
}

func NewCommandHandler(apps *service.AppService, servers *service.ServerService) *CommandHandler {
	return &CommandHandler{apps: apps, servers: servers}
}

// Items are the workspace's apps with their actions, servers and buckets,
// loaded on the first open of ⌘K.
func (h *CommandHandler) Items(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	apps, err := h.apps.List(workspace.ID)
	if err != nil {
		slog.Error("command items failed", "error", err, "workspace_id", workspace.ID)
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	servers, err := h.servers.List(workspace.ID)
	if err != nil {
		slog.Error("command items failed", "error", err, "workspace_id", workspace.ID)
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	buckets, _ := h.apps.Buckets(workspace.ID)
	view := pages.CommandItemsView{Apps: apps, Servers: servers, Buckets: buckets, CanChange: h.servers.RequireChanges(workspace.ID) == nil}
	w.Header().Set("Cache-Control", "no-store")
	render(w, r, pages.CommandItems(view))
}
