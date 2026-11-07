package handler

import (
	"net/http"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
)

// UpdateHandler is the update notice of the web UI: a new release for the
// admins of a self-hosted control plane, and "Update now".
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

// Start asks the updater for the newest release and answers the notice,
// which follows the update.
func (h *UpdateHandler) Start(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if _, err := h.updates.Start(actor(r), user); err != nil {
		answerFailed(w, r, "Update not started", errorMessage(err))
	}
	render(w, r, pages.UpdateNotice(h.updates.Info(user)))
}
