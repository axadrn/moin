package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/layouts"
)

type ProfileHandler struct {
	users *service.UserService
}

func NewProfileHandler(users *service.UserService) *ProfileHandler {
	return &ProfileHandler{users: users}
}

func (h *ProfileHandler) UpdateName(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	name := r.FormValue("name")
	if strings.TrimSpace(name) == "" {
		fieldError(w, "Name not saved", "name", "Enter your name.")
		return
	}
	updatedUser, err := h.users.UpdateName(user.ID, name)
	if err != nil {
		slog.Error("failed to update name", "error", err, "user_id", user.ID)
		toastFailed(w, r, "Name not saved", "Something went wrong. Please try again.")
		return
	}
	render(w, r, layouts.SidebarIdentityUpdates(updatedUser))
	toastDone(w, r, "Name saved", "Your team sees it in ops and in the activity.")
}
