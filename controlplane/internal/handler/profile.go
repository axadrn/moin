package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/layouts"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
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

// EmailAlerts turns the user's email alerts on or off. The switch shows
// the new state already; a failed save flips it back.
func (h *ProfileHandler) EmailAlerts(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	on := r.FormValue("on") == "1"
	err := h.users.SetEmailAlerts(user.ID, on)
	if err != nil {
		slog.Error("failed to set email alerts", "error", err, "user_id", user.ID)
		toastFailed(w, r, "Setting not saved", "Something went wrong. Please try again.")
		renderOOB(w, r, pages.EmailAlertsForm(user.EmailAlerts), "outerHTML:#email-alerts-form")
		return
	}
	if on {
		toastDone(w, r, "Email alerts on", "You get a mail when a site goes down, a server goes offline or a deploy fails.")
	} else {
		toastDone(w, r, "Email alerts off", "You no longer get alert mails.")
	}
}
