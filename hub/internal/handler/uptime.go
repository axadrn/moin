package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// Uptime is the uptime page of an app.
func (h *AppsHandler) Uptime(w http.ResponseWriter, r *http.Request) {
	h.renderUptime(w, r)
}

// UptimeCheck saves the path of a domain's check.
func (h *AppsHandler) UptimeCheck(w http.ResponseWriter, r *http.Request) {
	domain := r.FormValue("domain")
	path := r.FormValue("path")
	_, _, err := h.apps.SetUptimeCheck(actor(r), r.PathValue("app"), domain, &path, nil)
	if err != nil {
		failed(w, r, "Check of "+domain+" not saved", err, nil)
		return
	}
	h.renderUptime(w, r)
	answerDone(w, r, "Path of "+domain+" saved", "The next check goes there.")
}

// UptimeOn is the switch that pauses or resumes the check of a domain: it
// saves only that and answers the switch as stored, so a failed save flips
// it back.
func (h *AppsHandler) UptimeOn(w http.ResponseWriter, r *http.Request) {
	domain := r.FormValue("domain")
	paused := r.FormValue("on") != "1"
	app, view, err := h.apps.SetUptimeCheck(actor(r), r.PathValue("app"), domain, nil, &paused)
	if err != nil {
		var found bool
		app, view, found = h.uptimeDomain(r, domain)
		if !found {
			failed(w, r, "Check of "+domain+" not saved", err, nil)
			return
		}
		answerError(w, r, "Check of "+domain+" not saved", errorMessage(err))
	}
	render(w, r, pages.UptimeSwitchForm(*app, view))
}

// uptimeDomain is the check of one domain of the app as stored.
func (h *AppsHandler) uptimeDomain(r *http.Request, domain string) (*model.App, service.UptimeDomain, bool) {
	app, uptime, err := h.apps.Uptime(ctxkeys.Workspace(r.Context()).ID, r.PathValue("app"))
	if err != nil {
		return nil, service.UptimeDomain{}, false
	}
	for _, check := range uptime.Domains {
		if check.Domain == domain {
			return app, check, true
		}
	}
	return nil, service.UptimeDomain{}, false
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
