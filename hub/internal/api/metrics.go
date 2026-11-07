package api

import (
	"net/http"
	"time"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/service"
)

// ServerMetrics is the answer of GET /api/v1/servers/{id}/metrics: the
// server now, its points over the range, and its apps now, the one with the
// most memory first.
type ServerMetrics struct {
	Server string `json:"server"`
	Online bool   `json:"online"`
	// Range is 1h, 24h, 7d or 30d; StepSeconds is how far apart the points
	// are: 15 for the samples of the last hour, else the step they average.
	Range       string             `json:"range"`
	StepSeconds int                `json:"step_seconds"`
	Now         *ServerMetricsNow  `json:"now"`
	Points      []ServerPoint      `json:"points"`
	Apps        []ServerAppMetrics `json:"apps"`
}

// ServerMetricsNow is the newest sample of a server, at most two minutes
// old. CPU is the busy share of all cores in percent, memory and disk in
// bytes, the network in bytes per second.
type ServerMetricsNow struct {
	At          time.Time `json:"at"`
	CPU         float64   `json:"cpu"`
	Cores       int       `json:"cores"`
	MemoryUsed  uint64    `json:"memory_used"`
	MemoryTotal uint64    `json:"memory_total"`
	DiskUsed    uint64    `json:"disk_used"`
	DiskTotal   uint64    `json:"disk_total"`
	NetRx       float64   `json:"net_rx"`
	NetTx       float64   `json:"net_tx"`
	Load        float64   `json:"load"`
}

// ServerPoint is a server at one point of a chart.
type ServerPoint struct {
	At     time.Time `json:"at"`
	CPU    float64   `json:"cpu"`
	Memory uint64    `json:"memory"`
	Disk   uint64    `json:"disk"`
	NetRx  float64   `json:"net_rx"`
	NetTx  float64   `json:"net_tx"`
	Load   float64   `json:"load"`
}

// ServerAppMetrics is an app on the server and its newest sample, null
// without one.
type ServerAppMetrics struct {
	App string         `json:"app"`
	Now *AppMetricsNow `json:"now"`
}

// AppMetrics is the answer of GET /api/v1/apps/{app}/metrics: the app now,
// its points over the range and the deploys that went live in it.
type AppMetrics struct {
	App          string         `json:"app"`
	Server       string         `json:"server"`
	ServerOnline bool           `json:"server_online"`
	Range        string         `json:"range"`
	StepSeconds  int            `json:"step_seconds"`
	Now          *AppMetricsNow `json:"now"`
	Points       []AppPoint     `json:"points"`
	Deploys      []DeployMark   `json:"deploys"`
}

// AppMetricsNow is the newest sample of an app's running containers added
// up. CPU is the app's share of the whole server in percent; a memory
// limit of 0 means none.
type AppMetricsNow struct {
	At          time.Time `json:"at"`
	CPU         float64   `json:"cpu"`
	MemoryUsed  uint64    `json:"memory_used"`
	MemoryLimit uint64    `json:"memory_limit"`
	NetRx       float64   `json:"net_rx"`
	NetTx       float64   `json:"net_tx"`
	Containers  int       `json:"containers"`
}

// AppPoint is an app at one point of a chart.
type AppPoint struct {
	At     time.Time `json:"at"`
	CPU    float64   `json:"cpu"`
	Memory uint64    `json:"memory"`
	NetRx  float64   `json:"net_rx"`
	NetTx  float64   `json:"net_tx"`
}

// DeployMark is a deploy that went live, to mark on the charts.
type DeployMark struct {
	Number int       `json:"number"`
	At     time.Time `json:"at"`
}

// NewServerMetrics is the JSON shape of a server's metrics.
func NewServerMetrics(server *model.Server, metrics service.ServerMetrics) ServerMetrics {
	answer := ServerMetrics{
		Server: server.Name, Online: metrics.Online, Range: metrics.Range.Name, StepSeconds: int(metrics.Range.Step / time.Second),
		Points: []ServerPoint{}, Apps: []ServerAppMetrics{},
	}
	if now := metrics.Now; now != nil {
		answer.Now = &ServerMetricsNow{At: now.At, CPU: round(now.CPU), Cores: now.Cores, MemoryUsed: now.MemoryUsed, MemoryTotal: now.MemoryTotal,
			DiskUsed: now.DiskUsed, DiskTotal: now.DiskTotal, NetRx: round(now.NetRx), NetTx: round(now.NetTx), Load: round(now.Load)}
	}
	for _, point := range metrics.Points {
		answer.Points = append(answer.Points, ServerPoint{At: point.At, CPU: round(point.CPU), Memory: point.Memory, Disk: point.Disk,
			NetRx: round(point.NetRx), NetTx: round(point.NetTx), Load: round(point.Load)})
	}
	for _, app := range metrics.Apps {
		answer.Apps = append(answer.Apps, ServerAppMetrics{App: app.Name, Now: newAppMetricsNow(app.Now)})
	}
	return answer
}

// NewAppMetrics is the JSON shape of an app's metrics.
func NewAppMetrics(app *model.App, metrics service.AppMetrics) AppMetrics {
	answer := AppMetrics{
		App: app.Name, Server: metrics.Server, ServerOnline: metrics.ServerOnline, Range: metrics.Range.Name,
		StepSeconds: int(metrics.Range.Step / time.Second), Now: newAppMetricsNow(metrics.Now), Points: []AppPoint{}, Deploys: []DeployMark{},
	}
	for _, point := range metrics.Points {
		answer.Points = append(answer.Points, AppPoint{At: point.At, CPU: round(point.CPU), Memory: point.Memory, NetRx: round(point.NetRx), NetTx: round(point.NetTx)})
	}
	for _, deploy := range metrics.Deploys {
		answer.Deploys = append(answer.Deploys, DeployMark{Number: deploy.Number, At: deploy.FinishedAt.UTC()})
	}
	return answer
}

func newAppMetricsNow(now *service.MetricsNow) *AppMetricsNow {
	if now == nil {
		return nil
	}
	return &AppMetricsNow{At: now.At, CPU: round(now.CPU), MemoryUsed: now.MemoryUsed, MemoryLimit: now.MemoryLimit,
		NetRx: round(now.NetRx), NetTx: round(now.NetTx), Containers: now.Containers}
}

// round keeps two decimals, enough for percent and bytes per second.
func round(value float64) float64 {
	return float64(int64(value*100+0.5)) / 100
}

// ServerMetrics returns the metrics of a server over ?range=1h, 24h, 7d or
// 30d.
func (h *AppsHandler) ServerMetrics(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	server, metrics, err := h.apps.ServerMetrics(workspace.ID, r.PathValue("id"), r.URL.Query().Get("range"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewServerMetrics(server, metrics))
}

// AppMetrics returns the metrics of an app over ?range=1h, 24h, 7d or 30d.
func (h *AppsHandler) AppMetrics(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, metrics, err := h.apps.AppMetrics(workspace.ID, r.PathValue("app"), r.URL.Query().Get("range"))
	if err != nil {
		ServiceError(w, err)
		return
	}
	JSON(w, http.StatusOK, NewAppMetrics(app, metrics))
}
