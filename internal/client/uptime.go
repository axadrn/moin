package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Uptime is the uptime of an app: one state for all its domains, its
// server, and each domain.
type Uptime struct {
	App          string         `json:"app"`
	State        string         `json:"state"`
	Server       string         `json:"server"`
	ServerOnline bool           `json:"server_online"`
	Domains      []UptimeDomain `json:"domains"`
}

// UptimeDomain is the uptime of one domain.
type UptimeDomain struct {
	Domain        string     `json:"domain"`
	Path          string     `json:"path"`
	CustomPath    bool       `json:"custom_path"`
	Paused        bool       `json:"paused"`
	State         string     `json:"state"`
	Since         *time.Time `json:"since"`
	CheckedAt     *time.Time `json:"checked_at"`
	StatusCode    int        `json:"status_code"`
	ResponseMS    int        `json:"response_ms"`
	Error         string     `json:"error"`
	CertExpiresAt *time.Time `json:"cert_expires_at"`
	Uptime24h     *float64   `json:"uptime_24h"`
	Uptime7d      *float64   `json:"uptime_7d"`
	Uptime30d     *float64   `json:"uptime_30d"`
	AverageMS     int        `json:"average_ms"`
	Outages       []Outage   `json:"outages"`
}

// Outage is a time a domain was down; EndedAt is nil while it lasts.
type Outage struct {
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Reason    string     `json:"reason"`
}

// Uptime returns the uptime of every domain of an app.
func (c *Client) Uptime(ctx context.Context, app string) (Uptime, error) {
	var uptime Uptime
	err := c.do(ctx, http.MethodGet, appPath(app, "uptime"), nil, &uptime)
	return uptime, err
}

// SetUptimeCheck changes the path of a domain's check, nil keeps it and ""
// goes back to the health path or /, and pauses or resumes it, nil keeps it.
func (c *Client) SetUptimeCheck(ctx context.Context, app, domain string, path *string, paused *bool) (UptimeDomain, error) {
	body := map[string]any{}
	if path != nil {
		body["path"] = *path
	}
	if paused != nil {
		body["paused"] = *paused
	}
	var answer UptimeDomain
	err := c.do(ctx, http.MethodPut, appPath(app, "uptime", url.PathEscape(domain)), body, &answer)
	return answer, err
}

// Percent is an uptime for people: "99.95%", or "-" without a check.
func Percent(value *float64) string {
	switch {
	case value == nil:
		return "-"
	case *value == 100:
		return "100%"
	}
	return strconv.FormatFloat(*value, 'f', 2, 64) + "%"
}
