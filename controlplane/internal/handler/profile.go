package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/layouts"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
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
	toastDone(w, r, "Name saved", "Your team sees it in moin and in the activity.")
}

// EmailAlerts turns the user's email alerts on or off and answers the
// switch as stored, so a failed save flips it back.
func (h *ProfileHandler) EmailAlerts(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	on := r.FormValue("on") == "1"
	err := h.users.SetEmailAlerts(user.ID, on)
	if err != nil {
		slog.Error("failed to set email alerts", "error", err, "user_id", user.ID)
		on = user.EmailAlerts
		toastError(w, r, "Setting not saved", "Something went wrong. Please try again.")
	}
	render(w, r, pages.EmailAlertsForm(on))
}
