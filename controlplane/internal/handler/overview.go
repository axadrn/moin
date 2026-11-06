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
}

func NewOverviewHandler(status *service.StatusService, apps *service.AppService, servers *service.ServerService) *OverviewHandler {
	return &OverviewHandler{status: status, apps: apps, servers: servers}
}

// Page shows what needs the member, then every app and server.
func (h *OverviewHandler) Page(w http.ResponseWriter, r *http.Request) {
	member := ctxkeys.WorkspaceMember(r.Context())
	view := pages.OverviewView{}
	var err error
	if view.Status, err = h.status.Status(member.WorkspaceID); err == nil {
		if view.Apps, err = h.apps.List(member.WorkspaceID); err == nil {
			view.Servers, err = h.servers.List(member.WorkspaceID)
		}
	}
	if err != nil {
		slog.Error("overview failed", "error", err, "workspace_id", member.WorkspaceID)
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "The overview didn't load", "Try again in a moment."))
		return
	}
	render(w, r, pages.Overview(view))
}
