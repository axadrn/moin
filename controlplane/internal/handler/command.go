package handler

import (
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
)

// CommandHandler answers the search in the bar.
type CommandHandler struct {
	apps    *service.AppService
	servers *service.ServerService
}

func NewCommandHandler(apps *service.AppService, servers *service.ServerService) *CommandHandler {
	return &CommandHandler{apps: apps, servers: servers}
}

// Search answers what matches q: apps, servers, buckets, places and
// actions, as the list under the search field (htmx active search).
func (h *CommandHandler) Search(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	apps, err := h.apps.List(workspace.ID)
	if err != nil {
		slog.Error("search failed", "error", err, "workspace_id", workspace.ID)
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	servers, err := h.servers.List(workspace.ID)
	if err != nil {
		slog.Error("search failed", "error", err, "workspace_id", workspace.ID)
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	buckets, _ := h.apps.Buckets(workspace.ID)
	member := ctxkeys.WorkspaceMember(r.Context())
	items := pages.SearchItems(pages.SearchInput{
		Base:      ctxkeys.AppBasePath(r.Context()),
		Apps:      apps,
		Servers:   servers,
		Buckets:   buckets,
		CanChange: h.servers.RequireChanges(workspace.ID) == nil,
		IsOwner:   member != nil && member.Can(model.WorkspacePermissionManageBilling) && ctxkeys.RenderConfig(r.Context()).Billing,
	})
	query := r.URL.Query().Get("q")
	found := pages.MatchSearch(items, query, 14)
	// Nothing typed: what you can do here comes first (which-key).
	if query == "" {
		here := pages.HereItems(pages.SearchInput{Base: ctxkeys.AppBasePath(r.Context()), Here: r.URL.Query().Get("here"), Apps: apps, CanChange: h.servers.RequireChanges(workspace.ID) == nil})
		found = append(here, found...)
	}
	w.Header().Set("Cache-Control", "no-store")
	render(w, r, pages.SearchResults(found, query))
}
