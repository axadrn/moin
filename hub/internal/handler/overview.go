package handler

import (
	"log/slog"
	"net/http"
	"sort"

	"github.com/axadrn/moin/hub/internal/model"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// OverviewHandler serves the first page of a workspace: is everything OK?
type OverviewHandler struct {
	status  *service.StatusService
	apps    *service.AppService
	servers *service.ServerService
}

func NewOverviewHandler(status *service.StatusService, apps *service.AppService, servers *service.ServerService) *OverviewHandler {
	return &OverviewHandler{status: status, apps: apps, servers: servers}
}

// Page shows what needs the member, their pinned apps and the ones
// deployed last.
func (h *OverviewHandler) Page(w http.ResponseWriter, r *http.Request) {
	member := ctxkeys.WorkspaceMember(r.Context())
	view, err := h.view(member.WorkspaceID)
	if err != nil {
		slog.Error("overview failed", "error", err, "workspace_id", member.WorkspaceID)
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "The overview didn't load", "Try again in a moment."))
		return
	}
	render(w, r, pages.Overview(view))
}

// recentApps is how many apps the overview lists as deployed last.
const recentApps = 5

func (h *OverviewHandler) view(workspaceID string) (pages.OverviewView, error) {
	view := pages.OverviewView{}
	var err error
	if view.Status, err = h.status.Status(workspaceID); err != nil {
		return view, err
	}
	if view.Servers, err = h.servers.List(workspaceID); err != nil {
		return view, err
	}
	apps, err := h.apps.List(workspaceID)
	if err != nil {
		return view, err
	}
	last, err := h.apps.LastDeploys(workspaceID)
	if err != nil {
		return view, err
	}
	view.Apps = len(apps)
	for _, app := range service.SortApps(apps, view.Servers) {
		if app.Pinned {
			view.Pinned = append(view.Pinned, app)
		}
	}
	recent := []model.App{}
	for _, app := range apps {
		if !app.Pinned && last[app.ID] > 0 {
			recent = append(recent, app)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool { return last[recent[i].ID] > last[recent[j].ID] })
	view.Recent = recent[:min(len(recent), recentApps)]
	return view, nil
}
