package handler

import (
	"log/slog"
	"net/http"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

type HomeHandler struct {
	telemetry  *service.TelemetryService
	billing    bool
	contentDir string
}

func NewHomeHandler(telemetry *service.TelemetryService, billing bool, contentDir string) *HomeHandler {
	return &HomeHandler{telemetry: telemetry, billing: billing, contentDir: contentDir}
}

// HomePage shows the landing page of the hosted hub. A self-hosted
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
	// A changelog that cannot be read hides Built in the open, nothing more.
	releases, err := service.ChangelogReleases(h.contentDir)
	if err != nil {
		slog.Warn("home: changelog", "error", err)
	}
	render(w, r, pages.Home(h.telemetry.Network(), pages.NewHomeOpen(releases)))
}

func (h *HomeHandler) NotFoundPage(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	render(w, r, pages.NotFound())
}
