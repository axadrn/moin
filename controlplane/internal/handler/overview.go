package handler

import (
	"log/slog"
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// OverviewHandler serves the first page of a workspace: is everything OK?
type OverviewHandler struct {
	status     *service.StatusService
	workspaces *service.WorkspaceService
}

func NewOverviewHandler(status *service.StatusService, workspaces *service.WorkspaceService) *OverviewHandler {
	return &OverviewHandler{status: status, workspaces: workspaces}
}

// Page shows what needs the member, what could be newer and what happened
// last.
func (h *OverviewHandler) Page(w http.ResponseWriter, r *http.Request) {
	member := ctxkeys.WorkspaceMember(r.Context())
	status, err := h.status.Status(member.WorkspaceID)
	if err != nil {
		slog.Error("overview failed", "error", err, "workspace_id", member.WorkspaceID)
		w.WriteHeader(http.StatusInternalServerError)
		render(w, r, pages.ErrorPage(http.StatusInternalServerError, "Overview unavailable", "The overview could not be loaded. Please try again."))
		return
	}
	view := pages.OverviewView{Status: status, SeeActivity: member.Can(model.WorkspacePermissionViewAuditLog)}
	if view.SeeActivity {
		view.Activity, _, err = h.workspaces.AuditEventsPage(member.WorkspaceID, model.ActivityFilter{}, "", 8)
		if err != nil {
			slog.Error("overview activity failed", "error", err, "workspace_id", member.WorkspaceID)
		}
	}
	render(w, r, pages.Overview(view))
}
