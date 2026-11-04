package handler

import (
	"encoding/json"
	"net/http"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/controlplane/internal/service"
)

// TelemetryHandler takes the counts self-hosted control planes send.
type TelemetryHandler struct {
	telemetry *service.TelemetryService
}

func NewTelemetryHandler(telemetry *service.TelemetryService) *TelemetryHandler {
	return &TelemetryHandler{telemetry: telemetry}
}

func (h *TelemetryHandler) Receive(w http.ResponseWriter, r *http.Request) {
	var report model.TelemetryReport
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&report)
	if err == nil {
		err = h.telemetry.Receive(report)
	}
	if err != nil {
		http.Error(w, "not a telemetry report", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
