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
	h.home(w, r, "a")
}

// HomeA and HomeB show the landing page with each of the two heroes that
// compete, side by side, until one wins.
func (h *HomeHandler) HomeA(w http.ResponseWriter, r *http.Request) { h.home(w, r, "a") }
func (h *HomeHandler) HomeB(w http.ResponseWriter, r *http.Request) { h.home(w, r, "b") }

func (h *HomeHandler) home(w http.ResponseWriter, r *http.Request, hero string) {
	if !h.billing {
		if ctxkeys.User(r.Context()) != nil {
			redirect(w, r, "/app", http.StatusSeeOther)
			return
		}
		redirect(w, r, "/auth", http.StatusSeeOther)
		return
	}
	render(w, r, pages.Home(h.telemetry.Network(), hero))
}

func (h *HomeHandler) NotFoundPage(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	render(w, r, pages.NotFound())
}
