package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
)

// CommandHandler answers the palette.
type CommandHandler struct {
	apps    *service.AppService
	servers *service.ServerService
	docs    *service.DocsService
}

func NewCommandHandler(apps *service.AppService, servers *service.ServerService, docs *service.DocsService) *CommandHandler {
	return &CommandHandler{apps: apps, servers: servers, docs: docs}
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
	_, gitHubErr := h.apps.GitHubInstallURL(actor(r))
	input := pages.SearchInput{
		Base:      ctxkeys.AppBasePath(r.Context()),
		Here:      r.URL.Query().Get("here"),
		Apps:      apps,
		Servers:   servers,
		Buckets:   buckets,
		CanChange: h.servers.RequireChanges(workspace.ID) == nil,
		IsOwner:   member != nil && member.Can(model.WorkspacePermissionManageBilling) && ctxkeys.RenderConfig(r.Context()).Billing,
		GitHub:    gitHubErr == nil,
	}
	if h.docs != nil {
		for _, page := range h.docs.FlatDocsList() {
			input.Docs = append(input.Docs, pages.SearchDoc{Title: page.Title, Group: page.Group, Href: "/docs/" + page.Slug})
		}
	}
	// recent are the IDs of the picks this browser remembers, the most
	// used and latest first (moin.js).
	var recent []string
	if value := r.URL.Query().Get("recent"); value != "" {
		recent = strings.Split(value, "\n")
	}
	query := r.URL.Query().Get("q")
	found := pages.MatchSearch(pages.SearchItems(input), pages.HereItems(input), recent, query, 14)
	w.Header().Set("Cache-Control", "no-store")
	render(w, r, pages.SearchResults(found, query))
}
