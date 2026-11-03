package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// Uptime is the uptime page of an app.
func (h *AppsHandler) Uptime(w http.ResponseWriter, r *http.Request) {
	h.renderUptime(w, r)
}

// UptimeCheck saves the path of a domain's check, or with pause its
// switch: on checks, off pauses.
func (h *AppsHandler) UptimeCheck(w http.ResponseWriter, r *http.Request) {
	domain := r.FormValue("domain")
	var path *string
	var paused *bool
	if r.FormValue("pause") != "" {
		off := r.FormValue("on") != "1"
		paused = &off
	} else {
		value := r.FormValue("path")
		path = &value
	}
	_, _, err := h.apps.SetUptimeCheck(actor(r), r.PathValue("app"), domain, path, paused)
	if err != nil {
		failed(w, r, "Check of "+domain+" not saved", err, nil)
		return
	}
	// The switch shows its state itself; only a new path redraws the panel.
	if paused == nil {
		h.renderUptime(w, r)
	}
	switch {
	case paused != nil && *paused:
		toastDone(w, r, "Check of "+domain+" paused", "Turn it on again to check every minute.")
	case paused != nil:
		toastDone(w, r, "Checking "+domain+" again", "The next check runs within a minute.")
	default:
		toastDone(w, r, "Path of "+domain+" saved", "The next check goes there.")
	}
}

func (h *AppsHandler) renderUptime(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, uptime, err := h.apps.Uptime(workspace.ID, r.PathValue("app"))
	if errors.Is(err, repository.ErrAppNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		renderError(w, "load uptime failed", err)
		return
	}
	render(w, r, pages.AppUptime(pages.UptimeView{App: *app, Uptime: uptime, Now: time.Now()}))
}
