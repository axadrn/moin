package handler

import (
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/service"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
)

type HomeHandler struct {
	telemetry *service.TelemetryService
}

func NewHomeHandler(telemetry *service.TelemetryService) *HomeHandler {
	return &HomeHandler{telemetry: telemetry}
}

func (h *HomeHandler) HomePage(w http.ResponseWriter, r *http.Request) {
	render(w, r, pages.Home(h.telemetry.Network()))
}

func (h *HomeHandler) NotFoundPage(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	render(w, r, pages.NotFound())
}
