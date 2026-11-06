package handler

import (
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
)

// OverviewHandler serves the first page of a workspace: is everything OK?
type OverviewHandler struct {
	status  *service.StatusService
	apps    *service.AppService
	servers *service.ServerService
	users   *service.UserService
}

func NewOverviewHandler(status *service.StatusService, apps *service.AppService, servers *service.ServerService, users *service.UserService) *OverviewHandler {
	return &OverviewHandler{status: status, apps: apps, servers: servers, users: users}
}

// Page shows what needs the member, then every app and server.
func (h *OverviewHandler) Page(w http.ResponseWriter, r *http.Request) {
	view, ok := h.view(w, r, ctxkeys.User(r.Context()).Sort())
	if ok {
		render(w, r, pages.Overview(view))
	}
}

// Sort sorts the apps another way for this member, on every surface, and
// answers only the apps of the overview (a templ fragment), which htmx
// swaps in place.
func (h *OverviewHandler) Sort(w http.ResponseWriter, r *http.Request) {
	how := r.FormValue("sort")
	if err := h.users.SetSort(ctxkeys.User(r.Context()).ID, how); err != nil {
		failed(w, r, "Sort not changed", err, nil)
		return
	}
	if view, ok := h.view(w, r, how); ok {
		renderFragment(w, r, pages.Overview(view), "overview-apps")
	}
}

// view is everything the overview shows, the apps sorted how.
func (h *OverviewHandler) view(w http.ResponseWriter, r *http.Request, how string) (pages.OverviewView, bool) {
	member := ctxkeys.WorkspaceMember(r.Context())
	view := pages.OverviewView{}
	var err error
	if view.Status, err = h.status.Status(member.WorkspaceID); err == nil {
		if view.Apps, err = h.apps.List(member.WorkspaceID); err == nil {
			view.Servers, err = h.servers.List(member.WorkspaceID)
		}
	}
	if err == nil {
		var last map[string]int64
		if last, err = h.apps.LastDeploys(member.WorkspaceID); err == nil {
			view.Sort = how
			view.Apps = service.SortApps(view.Apps, view.Sort, view.Servers, last)
		}
	}
	if err != nil {
		slog.Error("overview failed", "error", err, "workspace_id", member.WorkspaceID)
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "The overview didn't load", "Try again in a moment."))
		return view, false
	}
	return view, true
}
