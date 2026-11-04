package handler

import (
	"net/http"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
)

type HomeHandler struct {
	telemetry *service.TelemetryService
	billing   bool
}

func NewHomeHandler(telemetry *service.TelemetryService, billing bool) *HomeHandler {
	return &HomeHandler{telemetry: telemetry, billing: billing}
}

// HomePage shows the landing page of the hosted control plane. A self-hosted
// one is private and has none: it sends people to the app or to sign in.
func (h *HomeHandler) HomePage(w http.ResponseWriter, r *http.Request) {
	if !h.billing {
		if ctxkeys.User(r.Context()) != nil {
			redirect(w, r, "/app", http.StatusSeeOther)
			return
		}
		redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}
	render(w, r, pages.Home(h.telemetry.Network()))
}

func (h *HomeHandler) NotFoundPage(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	render(w, r, pages.NotFound())
}
