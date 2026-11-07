package handler

import (
	"errors"
	"net/http"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// AppMetrics is the metrics tab of an app, ?range=1h, 24h, 7d or 30d.
func (h *AppsHandler) AppMetrics(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, metrics, err := h.apps.AppMetrics(workspace.ID, r.PathValue("app"), metricsRange(r))
	if errors.Is(err, repository.ErrAppNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		renderError(w, "load metrics failed", err)
		return
	}
	render(w, r, pages.AppMetricsPage(pages.AppMetricsView{App: *app, Metrics: metrics}))
}

// ServerMetrics is the metrics page of a server.
func (h *AppsHandler) ServerMetrics(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	server, metrics, err := h.apps.ServerMetrics(workspace.ID, r.PathValue("id"), metricsRange(r))
	if errors.Is(err, repository.ErrServerNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		renderError(w, "load metrics failed", err)
		return
	}
	render(w, r, pages.ServerMetricsPage(pages.ServerMetricsView{Server: *server, Metrics: metrics}))
}

// metricsRange is the range asked for; an unknown one shows the last hour.
func metricsRange(r *http.Request) string {
	name := r.URL.Query().Get("range")
	for _, known := range service.MetricsRanges {
		if known.Name == name {
			return name
		}
	}
	return ""
}
