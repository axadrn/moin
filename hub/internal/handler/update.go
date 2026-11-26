package handler

import (
	"net/http"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// UpdateHandler is the update notice of the web UI: a new release for the
// admins of a self-hosted hub.
type UpdateHandler struct {
	updates *service.UpdateService
}

func NewUpdateHandler(updates *service.UpdateService) *UpdateHandler {
	return &UpdateHandler{updates: updates}
}

// Notice answers the notice, empty for anyone but an admin.
func (h *UpdateHandler) Notice(w http.ResponseWriter, r *http.Request) {
	render(w, r, pages.UpdateNotice(h.updates.Info(ctxkeys.User(r.Context()))))
}
