package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ServerMetrics are the metrics of a server: now, over a range, and its
// apps now, the one with the most memory first.
type ServerMetrics struct {
	Server      string             `json:"server"`
	Online      bool               `json:"online"`
	Range       string             `json:"range"`
	StepSeconds int                `json:"step_seconds"`
	Now         *ServerMetricsNow  `json:"now"`
	Points      []MetricsPoint     `json:"points"`
	Apps        []ServerAppMetrics `json:"apps"`
}

// ServerMetricsNow is the newest sample of a server.
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

// MetricsPoint is a server or an app at one point; apps have no disk and
// no load.
type MetricsPoint struct {
	At     time.Time `json:"at"`
	CPU    float64   `json:"cpu"`
	Memory uint64    `json:"memory"`
	Disk   uint64    `json:"disk"`
	NetRx  float64   `json:"net_rx"`
	NetTx  float64   `json:"net_tx"`
	Load   float64   `json:"load"`
}

// ServerAppMetrics is an app on a server and its newest sample, nil when it
// does not run.
type ServerAppMetrics struct {
	App string         `json:"app"`
	Now *AppMetricsNow `json:"now"`
}

// AppMetrics are the metrics of an app: now, over a range, and the deploys
// that went live in it.
type AppMetrics struct {
	App          string         `json:"app"`
	Server       string         `json:"server"`
	ServerOnline bool           `json:"server_online"`
	Range        string         `json:"range"`
	StepSeconds  int            `json:"step_seconds"`
	Now          *AppMetricsNow `json:"now"`
	Points       []MetricsPoint `json:"points"`
	Deploys      []DeployMark   `json:"deploys"`
}

// AppMetricsNow is the newest sample of an app; a MemoryLimit of 0 is none.
type AppMetricsNow struct {
	At          time.Time `json:"at"`
	CPU         float64   `json:"cpu"`
	MemoryUsed  uint64    `json:"memory_used"`
	MemoryLimit uint64    `json:"memory_limit"`
	NetRx       float64   `json:"net_rx"`
	NetTx       float64   `json:"net_tx"`
	Containers  int       `json:"containers"`
}

// DeployMark is a deploy that went live.
type DeployMark struct {
	Number int       `json:"number"`
	At     time.Time `json:"at"`
}

// AppMetrics returns the metrics of an app over a range: 1h, 24h, 7d or
// 30d, "" for 1h.
func (c *Client) AppMetrics(ctx context.Context, app, rangeName string) (AppMetrics, error) {
	var metrics AppMetrics
	err := c.do(ctx, http.MethodGet, appPath(app, "metrics")+metricsQuery(rangeName), nil, &metrics)
	return metrics, err
}

// ServerMetrics returns the metrics of a server over a range.
func (c *Client) ServerMetrics(ctx context.Context, server, rangeName string) (ServerMetrics, error) {
	var metrics ServerMetrics
	err := c.do(ctx, http.MethodGet, "/api/v1/servers/"+url.PathEscape(server)+"/metrics"+metricsQuery(rangeName), nil, &metrics)
	return metrics, err
}

func metricsQuery(rangeName string) string {
	if rangeName == "" {
		return ""
	}
	return "?range=" + url.QueryEscape(rangeName)
}

// Sparkline draws values as a line of block characters from the lowest to
// the highest, at most width of them: more values average into width.
func Sparkline(values []float64, width int) string {
	if len(values) > width && width > 0 {
		averaged := make([]float64, width)
		for index := range averaged {
			from, to := index*len(values)/width, (index+1)*len(values)/width
			sum := 0.0
			for _, value := range values[from:to] {
				sum += value
			}
			averaged[index] = sum / float64(to-from)
		}
		values = averaged
	}
	if len(values) == 0 {
		return ""
	}
	bars := []rune("▁▂▃▄▅▆▇█")
	low, high := values[0], values[0]
	for _, value := range values {
		low, high = min(low, value), max(high, value)
	}
	line := make([]rune, len(values))
	for index, value := range values {
		level := 0
		if high > low {
			level = int((value - low) / (high - low) * float64(len(bars)-1))
		}
		line[index] = bars[level]
	}
	return string(line)
}

// Share is used of total in percent, like 42%; "-" without a total.
func Share(used, total float64) string {
	if total <= 0 {
		return "-"
	}
	return strconv.FormatFloat(100*used/total, 'f', 0, 64) + "%"
}
