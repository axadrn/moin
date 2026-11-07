package protocol

import "time"

// MetricsEvery is how often the daemon samples its server and apps and sends
// the sample.
const MetricsEvery = 15 * time.Second

// Metrics is one sample of a server and the apps on it. Rates are per
// second since the sample before. Server is nil where the daemon cannot read
// the server's numbers, such as outside Linux.
type Metrics struct {
	At     time.Time      `json:"at"`
	Server *ServerMetrics `json:"server,omitempty"`
	Apps   []AppMetrics   `json:"apps,omitempty"`
}

// ServerMetrics are the numbers of the whole server. Disk is the disk that
// holds the apps' data.
type ServerMetrics struct {
	// CPU is the busy share of all cores in percent, 0 to 100.
	CPU         float64 `json:"cpu"`
	Cores       int     `json:"cores,omitempty"`
	MemoryUsed  uint64  `json:"memory_used"`
	MemoryTotal uint64  `json:"memory_total"`
	DiskUsed    uint64  `json:"disk_used"`
	DiskTotal   uint64  `json:"disk_total"`
	// NetRx and NetTx are bytes per second in and out over the server's
	// own interfaces, not Docker's bridges.
	NetRx float64 `json:"net_rx"`
	NetTx float64 `json:"net_tx"`
	// Load is the load average over one minute.
	Load float64 `json:"load"`
}

// AppMetrics are the numbers of the running containers of one app, added
// up.
type AppMetrics struct {
	App string `json:"app"`
	// CPU is the app's share of the whole server in percent, so the apps
	// add up to at most the server's CPU.
	CPU        float64 `json:"cpu"`
	MemoryUsed uint64  `json:"memory_used"`
	// MemoryLimit is the sum of the containers' limits; 0 means no limit,
	// so the server's memory is the limit.
	MemoryLimit uint64  `json:"memory_limit,omitempty"`
	NetRx       float64 `json:"net_rx"`
	NetTx       float64 `json:"net_tx"`
	Containers  int     `json:"containers"`
}
