package handler

import (
	"errors"
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

// AlertsHandler serves the alerts page: where the alerts of the workspace
// go besides email.
type AlertsHandler struct {
	alerts *service.AlertService
}

func NewAlertsHandler(alerts *service.AlertService) *AlertsHandler {
	return &AlertsHandler{alerts: alerts}
}

// alertFields are the fields of the destination form, by API field name.
var alertFields = map[string]string{"kind": "alert-kind", "name": "alert-name", "url": "alert-url", "bot_token": "alert-bot-token", "chat_id": "alert-chat-id"}

// Page is the alerts page.
func (h *AlertsHandler) Page(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, pages.AlertsView{})
}

// Add stores a destination. A webhook's signing secret shows this once.
func (h *AlertsHandler) Add(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		toastFailed(w, r, "Destination not added", "The form could not be read.")
		return
	}
	destination, signingSecret, err := h.alerts.AddDestination(ctxkeys.WorkspaceMember(r.Context()), service.RequestSource(r.Context()), service.AlertDestinationInput{
		Kind: r.FormValue("kind"), Name: r.FormValue("name"), URL: r.FormValue("url"),
		BotToken: r.FormValue("bot_token"), ChatID: r.FormValue("chat_id"), Events: r.Form["events"],
	})
	if err != nil {
		failed(w, r, "Destination not added", err, alertFields)
		return
	}
	h.render(w, r, pages.AlertsView{Created: &destination, SigningSecret: signingSecret})
	toastDone(w, r, destination.Name+" added", "Send it a test to see that it works.")
}

// Remove deletes a destination.
func (h *AlertsHandler) Remove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("destination")
	if err := h.alerts.RemoveDestination(ctxkeys.WorkspaceMember(r.Context()), service.RequestSource(r.Context()), name); err != nil {
		failed(w, r, "Destination not removed", err, nil)
		return
	}
	h.render(w, r, pages.AlertsView{})
	toastDone(w, r, name+" removed", "Its alerts stop.")
}

// Test sends a test alert. The list shows the result either way.
func (h *AlertsHandler) Test(w http.ResponseWriter, r *http.Request) {
	destination, err := h.alerts.TestDestination(r.Context(), ctxkeys.WorkspaceMember(r.Context()), service.RequestSource(r.Context()), r.PathValue("destination"))
	var sendErr *service.AlertSendError
	switch {
	case errors.As(err, &sendErr):
		h.render(w, r, pages.AlertsView{})
		toastError(w, r, "Test not taken", destination.Name+": "+sendErr.Message)
	case err != nil:
		failed(w, r, "Test not sent", err, nil)
	default:
		h.render(w, r, pages.AlertsView{})
		toastDone(w, r, "Test sent", destination.Name+" took it. Look for it there.")
	}
}

func (h *AlertsHandler) render(w http.ResponseWriter, r *http.Request, view pages.AlertsView) {
	destinations, err := h.alerts.Destinations(ctxkeys.Workspace(r.Context()).ID)
	if err != nil {
		renderError(w, "list alert destinations failed", err)
		return
	}
	view.Destinations = destinations
	render(w, r, pages.Alerts(view))
}
